package scrape

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/domain"
	subtitlemeta "github.com/ppxb/miyabi/internal/domain/subtitle"
	"github.com/ppxb/miyabi/internal/drive"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/file"
	"github.com/ppxb/miyabi/internal/ent/movie"
	"github.com/ppxb/miyabi/internal/export"
	"github.com/ppxb/miyabi/internal/pan"
	"github.com/ppxb/miyabi/internal/tasks"
)

func (service *Service) publishMovie(ctx context.Context, sess drive.Session, job tasks.Job, input Payload) (*SubtitleTask, error) {
	// The unfinished task retains these cache files during remote queries
	// and local export, until the final transaction publishes movie references.
	artwork := *input.Artwork
	poster, err := service.images.ReadURL(artwork.Poster)
	if err != nil {
		return nil, fmt.Errorf("read cached poster: %w", err)
	}
	fanart, err := service.images.ReadURL(artwork.Fanart)
	if err != nil {
		return nil, fmt.Errorf("read cached fanart: %w", err)
	}
	// Verify current video locations before exporting local files.
	directories, err := service.Directories(ctx, sess, input.MetadataPayload)
	if err != nil {
		return nil, err
	}
	snapshot := &domain.MetadataSnapshot{
		Code:          input.Code,
		AccountID:     input.Source.AccountID,
		DirectoryID:   input.Source.Directory.ID,
		PosterVersion: input.PosterVersion,
	}
	var videos []pan.File
	for i, directory := range directories {
		if err := service.verifyVideoPositions(ctx, sess, directory); err != nil {
			return nil, err
		}
		for _, entry := range directory.Files {
			if directory.VideoIDs[entry.ID] {
				videos = append(videos, entry)
			}
		}
		if err := service.db.Task.UpdateOneID(job.ID).SetProgress((i + 1) * 100 / len(directories)).Exec(ctx); err != nil {
			return nil, err
		}
	}
	// Deduplicate and sort all videos across all directories
	seenVideos := make(map[string]bool, len(videos))
	uniqueVideos := make([]pan.File, 0, len(videos))
	for _, v := range videos {
		if !seenVideos[v.ID] {
			seenVideos[v.ID] = true
			uniqueVideos = append(uniqueVideos, v)
		}
	}
	videos = uniqueVideos
	snapshot.Videos = VideoFingerprint(videos)
	poster, err = service.decorateArtwork(ctx, job.ID, &input, videos, poster)
	if err != nil {
		return nil, err
	}
	artwork = *input.Artwork

	input.Completed = true
	encoded, err := tasks.EncodePayload(input)
	if err != nil {
		return nil, err
	}
	if err := service.exportMgr.WithConfig(func(cfg export.Config) error {
		return sess.WithSource(ctx, func() error {
			record, err := service.db.Movie.Query().Where(movie.IDEQ(input.MovieID)).
				Select(movie.FieldID, movie.FieldManualCode, movie.FieldMetadataSnapshot).Only(ctx)
			if err != nil {
				return err
			}
			if record.ManualCode != input.ManualCode {
				return domain.E(domain.KindConflict, "影片番号已纠正，请使用新的刮削任务", nil)
			}
			// A scan may have reconciled the index while artwork was downloading.
			// Check again under the source/export locks before creating any sidecars.
			files, err := service.db.File.Query().Where(database.LibraryFiles(input.Source), file.MovieIDEQ(input.MovieID)).All(ctx)
			if err != nil {
				return err
			}
			current := make([]pan.File, 0, len(files))
			for _, entry := range files {
				current = append(current, pan.File{ID: entry.FileID, ParentID: entry.ParentID, Name: entry.Name, Size: entry.Size, SHA1: entry.Sha1})
			}
			if len(current) == 0 {
				return domain.E(domain.KindConflict, "媒体文件索引已变化，请重新扫描", nil)
			}
			if VideoFingerprint(current) != snapshot.Videos {
				// A shared task may overlap a rescan that adds or moves a part.
				// Retry publication from saved artwork using fresh directory listings.
				service.dirMu.Lock()
				clear(service.dirCache)
				service.dirMu.Unlock()
				return &domain.RetryError{Cause: domain.E(domain.KindConflict, "媒体文件索引已更新，等待重新导出", nil)}
			}
			if _, err := export.WriteMedia(cfg, input.Code, input.Document, videos, poster, fanart); err != nil {
				return err
			}
			oldDir, err := service.removePreviousExport(ctx, cfg.EmbyDir, record, input.Code)
			if err != nil {
				return err
			}
			return ent.WithTx(ctx, service.db, func(tx *ent.Tx) error {
				// Decoration adds resource tags after the metadata checkpoint.
				// Publish their relational index and final document atomically.
				if input.Document.ArtworkExtension != nil {
					if err := SaveMovieMetadata(ctx, tx, input.MovieID, input.Document); err != nil {
						return err
					}
				}
				update := tx.Movie.UpdateOneID(input.MovieID).SetCode(input.Code).SetMetadata(&input.Document).
					SetCover(artwork.Thumbnail).SetPoster(artwork.Poster).SetFanarts([]string{artwork.Fanart}).
					SetScrapeStatus(movie.ScrapeStatusDone).SetMetadataSnapshot(snapshot)
				if id := input.Document.JavDBID(); id != "" {
					update.SetJavdbID(id)
				}
				if err := update.Exec(ctx); err != nil {
					return err
				}
				if err := tx.Task.UpdateOneID(job.ID).SetPayload(encoded).Exec(ctx); err != nil {
					return err
				}
				if service.mediaNotifier != nil && cfg.EmbyDir != "" {
					if oldDir != "" {
						if err := service.mediaNotifier.NotifyUpdatedTx(ctx, tx, oldDir); err != nil {
							return err
						}
					}
					return service.mediaNotifier.NotifyUpdatedTx(ctx, tx, export.EmbyMovieDir(cfg.EmbyDir, input.Code))
				}
				return nil
			})
		})
	}); err != nil {
		return nil, fmt.Errorf("publish movie: %w", err)
	}

	subTask := service.subtitleTask(input.MetadataPayload, videos)

	if service.notifier != nil {
		service.notifier.NotifyLibraryChanged()
	}
	return subTask, nil
}

// Each complete version has its own subtitle target. Whole-movie subtitles
// never attach to fragments, including an isolated single remaining disc.
func (service *Service) subtitleTask(input MetadataPayload, videos []pan.File) *SubtitleTask {
	if service.subtitles == nil {
		return nil
	}
	plan, err := export.PlanVideos(input.Code, videos)
	if err != nil {
		return nil // Publication already rejects invalid plans before writing.
	}
	task := &SubtitleTask{MovieID: input.MovieID}
	for _, video := range plan {
		if video.Part != 0 {
			continue
		}
		target := subtitlemeta.Target{
			Dir:           filepath.Join(export.EmbyMovieDir(service.exportConfig().EmbyDir, input.Code), video.Dir),
			Stem:          video.Stem,
			Code:          input.Code,
			Uncensored:    subtitlemeta.IsUncensored(video.File.Name),
			HardSubtitled: subtitlemeta.HasHardSubtitle(video.File.Name),
		}
		if len(plan) > 1 {
			target.Version = subtitlemeta.DetectVersion(video.File.Name)
		}
		task.Targets = append(task.Targets, target)
	}
	if len(task.Targets) == 0 {
		return nil
	}
	return task
}

func (service *Service) verifyVideoPositions(ctx context.Context, sess drive.Session, directory MovieDirectory) error {
	for videoID := range directory.VideoIDs {
		info, err := sess.Info(ctx, videoID)
		if err != nil {
			return fmt.Errorf("确认视频文件位置: %w", err)
		}
		if info.ParentID != directory.ID || !drive.WithinSource(info, sess.Source()) {
			return domain.E(domain.KindConflict, "视频已移动，请重新扫描", nil)
		}
	}
	return nil
}
