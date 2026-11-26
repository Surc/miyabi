package library

import (
	"context"
	"log/slog"
	"slices"
	"strings"

	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/domain"
	subtitlemeta "github.com/ppxb/miyabi/internal/domain/subtitle"
	"github.com/ppxb/miyabi/internal/drive"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/file"
	"github.com/ppxb/miyabi/internal/ent/offlinedownload"
	"github.com/ppxb/miyabi/internal/ent/predicate"
	"github.com/ppxb/miyabi/internal/pan"
)

type MovieVideo struct {
	ID            int                   `json:"id"`
	Name          string                `json:"name"`
	Size          int64                 `json:"size"`
	CanDelete     bool                  `json:"can_delete"`
	HardSubtitled bool                  `json:"hard_subtitled"`
	DownloadLinks []domain.DownloadLink `json:"download_links,omitempty"`
}

func movieVideos(ctx context.Context, record *ent.Movie, scope predicate.File, mounts []domain.LibrarySource) ([]MovieVideo, error) {
	files, err := record.QueryFiles().Where(scope).Order(file.ByName(), file.ByID()).All(ctx)
	if err != nil {
		return nil, err
	}
	videos := make([]MovieVideo, 0, len(files))
	for _, video := range files {
		videos = append(videos, MovieVideo{
			ID: video.ID, Name: video.Name, Size: video.Size, DownloadLinks: video.DownloadLinks,
			CanDelete: slices.ContainsFunc(mounts, func(source domain.LibrarySource) bool {
				return video.AccountID == source.AccountID && video.RootID == source.Directory.ID
			}),
			HardSubtitled: subtitlemeta.HasHardSubtitle(video.Name),
		})
	}
	return videos, nil
}

type VideoSourceBackfill struct {
	VideosUpdated int  `json:"videos_updated"`
	LinksAdded    int  `json:"links_added"`
	HistoryRead   bool `json:"history_read"`
}

func (s *Service) BackfillVideoSources(ctx context.Context) (VideoSourceBackfill, error) {
	mounts, err := s.drive.Mounts(ctx)
	if err != nil {
		return VideoSourceBackfill{}, err
	}
	if len(mounts) == 0 {
		return VideoSourceBackfill{}, domain.E(domain.KindInvalid, "请先挂载 115 媒体目录", nil)
	}
	result := VideoSourceBackfill{HistoryRead: true}
	for _, source := range mounts {
		current, err := s.backfillVideoSource(ctx, source)
		if err != nil {
			return result, err
		}
		result.HistoryRead = result.HistoryRead && current.HistoryRead
		result.LinksAdded += current.LinksAdded
		result.VideosUpdated += current.VideosUpdated
	}
	if result.LinksAdded > 0 && s.tasks != nil {
		s.tasks.NotifyLibraryChanged()
	}
	return result, nil
}

func (s *Service) backfillVideoSource(ctx context.Context, source domain.LibrarySource) (VideoSourceBackfill, error) {
	sess, err := s.drive.OpenSource(ctx, source)
	if err != nil {
		return VideoSourceBackfill{}, err
	}
	var history []pan.OfflineTask
	err = drive.WalkOfflinePages(ctx, func(page int) (pan.OfflinePage, error) {
		return sess.OfflineTasks(ctx, page)
	}, func(page pan.OfflinePage) (bool, error) {
		history = append(history, page.Tasks...)
		return true, nil
	})
	if ctx.Err() != nil {
		return VideoSourceBackfill{}, ctx.Err()
	}
	result := VideoSourceBackfill{HistoryRead: err == nil}
	if err != nil {
		slog.WarnContext(ctx, "115 history unavailable for video source backfill", "error", err)
		history = nil
	}
	err = sess.Commit(ctx, func(tx *ent.Tx) error {
		videos, err := tx.File.Query().Where(database.LibraryFiles(source), file.MovieIDNotNil()).All(ctx)
		if err != nil {
			return err
		}
		known, err := tx.OfflineDownload.Query().Where(
			offlinedownload.AccountIDEQ(source.AccountID),
			offlinedownload.DirectoryIDEQ(source.Directory.ID),
			offlinedownload.StatusEQ(offlinedownload.StatusDone),
		).Order(offlinedownload.ByID()).All(ctx)
		if err != nil {
			return err
		}
		links := backfillDownloadLinks(source.Directory.ID, videos, known, history)
		for _, video := range videos {
			resources := links[video.FileID]
			if len(resources) == 0 {
				continue
			}
			added, err := database.AddDownloadLinks(ctx, tx, video, resources)
			if err != nil {
				return err
			}
			if added > 0 {
				result.VideosUpdated++
				result.LinksAdded += added
			}
		}
		return nil
	})
	return result, err
}

func backfillDownloadLinks(rootID string, videos []*ent.File, known []*ent.OfflineDownload, history []pan.OfflineTask) map[string][]domain.DownloadLink {
	byID := make(map[string]*ent.File, len(videos))
	byParent := make(map[string][]*ent.File)
	for _, video := range videos {
		byID[video.FileID] = video
		byParent[video.ParentID] = append(byParent[video.ParentID], video)
	}
	links := make(map[string][]domain.DownloadLink)
	add := func(id string, link domain.DownloadLink) {
		if byID[id] != nil {
			links[id] = append(links[id], link)
		}
	}
	for _, record := range known {
		if link, ok := database.OfflineDownloadLink(record); ok {
			for _, id := range record.FileIds {
				add(id, link)
			}
			add(record.FileID, link)
		}
	}
	// A directory shared by tasks cannot prove ownership of its members.
	outputs := make(map[string]map[string]bool)
	for _, task := range history {
		if task.FileID != "" {
			if outputs[task.FileID] == nil {
				outputs[task.FileID] = make(map[string]bool)
			}
			link, _ := domain.ParseDownloadLink(task.URL)
			outputs[task.FileID][link.Key] = true
		}
	}
	for _, task := range history {
		if task.Status != 2 || task.FileID == "" || task.FileID == "0" {
			continue
		}
		link, ok := domain.ParseDownloadLink(task.URL)
		if !ok {
			continue
		}
		link.FromHistory = true
		if entry := byID[task.FileID]; entry != nil {
			if ed2k, ok := domain.ParseED2K(link.URI); !ok || entry.Size == ed2k.Size {
				add(entry.FileID, link)
			}
			continue
		}
		children := byParent[task.FileID]
		if len(children) == 0 || len(outputs[task.FileID]) != 1 {
			continue
		}
		if ed2k, ok := domain.ParseED2K(link.URI); ok {
			var match string
			for _, child := range children {
				if child.Size == ed2k.Size && strings.EqualFold(child.Name, ed2k.Name) {
					if match != "" {
						match = ""
						break
					}
					match = child.FileID
				}
			}
			add(match, link)
			continue
		}
		if task.DirectoryID != "" && task.FileID != task.DirectoryID && task.FileID != rootID {
			for _, child := range children {
				add(child.FileID, link)
			}
		}
	}
	return links
}
