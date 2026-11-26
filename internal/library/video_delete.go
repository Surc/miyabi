package library

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"time"

	"entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/file"
	"github.com/ppxb/miyabi/internal/ent/movie"
	"github.com/ppxb/miyabi/internal/ent/offlinedownload"
	"github.com/ppxb/miyabi/internal/ent/subtitle"
	"github.com/ppxb/miyabi/internal/export"
)

type VideoDeletion struct {
	MovieRemoved bool `json:"movie_removed"`
}

func (s *Service) DeleteMovieVideo(ctx context.Context, movieID, videoID int) (VideoDeletion, error) {
	if err := s.scanLock.Lock(ctx); err != nil {
		return VideoDeletion{}, err
	}
	defer s.scanLock.Unlock()
	mounts := s.mountList(ctx)
	scope := cloudFileScope(mounts)
	if scope == nil {
		return VideoDeletion{}, domain.E(domain.KindNotFound, "视频不在当前 115 媒体库中", nil)
	}
	var result VideoDeletion
	err := s.exportMgr.WithConfig(func(cfg export.Config) error {
		record, err := s.database.Movie.Query().Where(movie.IDEQ(movieID), movie.HasFilesWith(scope, file.IDEQ(videoID))).
			WithFiles(func(q *ent.FileQuery) { q.Where(fileScope(mounts)) }).Only(ctx)
		if ent.IsNotFound(err) {
			return domain.E(domain.KindNotFound, "视频不在当前 115 媒体库中", err)
		}
		if err != nil {
			return err
		}
		index := slices.IndexFunc(record.Edges.Files, func(f *ent.File) bool { return f.ID == videoID })
		if index < 0 {
			return domain.E(domain.KindNotFound, "视频不在当前 115 媒体库中", nil)
		}
		video := record.Edges.Files[index]
		mount := slices.IndexFunc(mounts, func(source domain.LibrarySource) bool {
			return video.AccountID == source.AccountID && video.RootID == source.Directory.ID
		})
		if mount < 0 {
			return domain.E(domain.KindNotFound, "视频不在当前 115 媒体库中", nil)
		}
		source := mounts[mount]
		sess, err := s.drive.OpenSource(ctx, source)
		if err != nil {
			return err
		}
		root, exported, err := s.videoSubtitleExport(ctx, cfg, record, videoID)
		if err != nil {
			return err
		}
		if root != nil {
			defer root.Close()
		}
		return sess.RecycleFile(ctx, video.FileID, func() error {
			// Once 115 has moved the file, finish updating the index even when
			// the requesting page closes.
			writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 18*time.Second)
			defer cancel()
			result, err = s.deleteVideoRecord(writeCtx, sess.Source(), record, video, root, exported)
			return err
		})
	})
	if err == nil && s.tasks != nil {
		s.tasks.NotifyLibraryChanged()
		s.tasks.NotifyOfflineChanged()
	}
	return result, err
}

func (s *Service) videoSubtitleExport(ctx context.Context, cfg export.Config, record *ent.Movie, videoID int) (*os.Root, SubtitleVideo, error) {
	if cfg.EmbyDir == "" {
		return nil, SubtitleVideo{}, nil
	}
	dir, err := canonicalDirectory(cfg.EmbyDir)
	if os.IsNotExist(err) {
		return nil, SubtitleVideo{}, nil
	}
	if err != nil {
		return nil, SubtitleVideo{}, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, SubtitleVideo{}, err
	}
	videos, err := s.subtitleVideos(ctx, root, cfg.EmbyDir, record)
	for _, video := range videos {
		if err == nil && video.ID == videoID {
			return root, video, nil
		}
	}
	root.Close()
	return nil, SubtitleVideo{}, err
}

func (s *Service) deleteVideoRecord(ctx context.Context, source domain.LibrarySource, record *ent.Movie, video *ent.File, root *os.Root, exported SubtitleVideo) (VideoDeletion, error) {
	var temporary string
	var trackIDs []int
	if exported.path != "" {
		temporary = filepath.Join(filepath.Dir(exported.path), ".miyabi-"+rand.Text()+".tmp")
		if err := root.Rename(exported.path, temporary); err != nil {
			return VideoDeletion{}, err
		}
		for _, track := range exported.Tracks {
			if track.recordID > 0 {
				trackIDs = append(trackIDs, track.recordID)
			}
		}
	}
	var result VideoDeletion
	err := ent.WithTx(ctx, s.database, func(tx *ent.Tx) error {
		if _, err := tx.Subtitle.Delete().Where(subtitle.MovieIDEQ(record.ID), subtitle.IDIn(trackIDs...)).Exec(ctx); err != nil {
			return err
		}
		if err := detachOfflineVideo(ctx, tx, source.AccountID, video.FileID); err != nil {
			return err
		}
		if err := tx.File.DeleteOneID(video.ID).Exec(ctx); err != nil {
			return err
		}
		count, err := tx.Movie.Delete().Where(movie.IDEQ(record.ID), movie.Not(movie.HasFiles())).Exec(ctx)
		if err != nil {
			return err
		}
		result.MovieRemoved = count != 0
		if temporary != "" && s.mediaNotifier != nil {
			return s.mediaNotifier.NotifyUpdatedTx(ctx, tx, filepath.Join(root.Name(), filepath.Dir(exported.path)))
		}
		return nil
	})
	if temporary != "" {
		if err != nil {
			return VideoDeletion{}, errors.Join(err, root.Rename(temporary, exported.path))
		}
		if cleanupErr := root.Remove(temporary); cleanupErr != nil {
			slog.WarnContext(ctx, "remove retired video export", "error", cleanupErr)
		}
	}
	return result, err
}

func detachOfflineVideo(ctx context.Context, tx *ent.Tx, accountID, fileID string) error {
	records, err := tx.OfflineDownload.Query().Where(
		offlinedownload.AccountIDEQ(accountID),
		offlinedownload.Or(offlinedownload.FileIDEQ(fileID), func(selector *sql.Selector) {
			selector.Where(sqljson.ValueContains(offlinedownload.FieldFileIds, fileID))
		}),
	).All(ctx)
	if err != nil {
		return err
	}
	for _, record := range records {
		update := tx.OfflineDownload.UpdateOneID(record.ID).SetFileIds(
			slices.DeleteFunc(record.FileIds, func(id string) bool { return id == fileID }),
		)
		if record.FileID == fileID {
			update.SetFileID("").SetAwaitingLocation(false)
		}
		if err := update.Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}
