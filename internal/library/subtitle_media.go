package library

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ppxb/miyabi/internal/domain"
	subtitlemeta "github.com/ppxb/miyabi/internal/domain/subtitle"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/movie"
	"github.com/ppxb/miyabi/internal/ent/subtitle"
	"github.com/ppxb/miyabi/internal/export"
	"github.com/ppxb/miyabi/internal/pan"
)

type SubtitleTrack struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Language string `json:"language,omitempty"`
	Format   string `json:"format"`
	Source   string `json:"source"`
	recordID int
}

type SubtitleVideo struct {
	ID            int             `json:"id"`
	Name          string          `json:"name"`
	CanDownload   bool            `json:"can_download"`
	HardSubtitled bool            `json:"hard_subtitled"`
	Tracks        []SubtitleTrack `json:"tracks"`
	rootDir       string
	path          string
	target        subtitlemeta.Target
}

func (s *Service) MovieSubtitles(ctx context.Context, id int) ([]SubtitleVideo, error) {
	var videos []SubtitleVideo
	err := s.withSubtitleMedia(ctx, id, func(_ *os.Root, _ string, media []SubtitleVideo) error {
		videos = media
		return nil
	})
	return videos, err
}

func (s *Service) withSubtitleMedia(ctx context.Context, id int, run func(*os.Root, string, []SubtitleVideo) error) error {
	return s.exportMgr.WithConfig(func(cfg export.Config) error {
		load := func(source *domain.LibrarySource) error {
			scope := libraryScope(source)
			record, err := s.database.Movie.Query().Where(movie.IDEQ(id), movie.HasFilesWith(scope)).
				WithFiles(func(query *ent.FileQuery) { query.Where(scope) }).Only(ctx)
			if ent.IsNotFound(err) {
				return domain.E(domain.KindNotFound, "影片不在当前媒体库中", err)
			}
			if err != nil {
				return err
			}
			if cfg.EmbyDir == "" {
				return run(nil, "", []SubtitleVideo{})
			}
			dir, err := canonicalDirectory(cfg.EmbyDir)
			if os.IsNotExist(err) {
				return run(nil, "", []SubtitleVideo{})
			}
			if err != nil {
				return err
			}
			root, err := os.OpenRoot(dir)
			if err != nil {
				return err
			}
			defer root.Close()
			videos, err := s.subtitleVideos(ctx, root, cfg.EmbyDir, record)
			if err != nil {
				return err
			}
			return run(root, dir, videos)
		}
		if s.drive != nil {
			return s.drive.WithCurrentSource(ctx, load)
		}
		return load(nil)
	})
}

func (s *Service) subtitleVideos(ctx context.Context, root *os.Root, configuredDir string, record *ent.Movie) ([]SubtitleVideo, error) {
	files := record.Edges.Files
	directory := export.EmbyMovieDir("", record.Code)
	type exportedVideo struct {
		path string
		part int
	}
	byID := make(map[string][]exportedVideo)
	if slices.ContainsFunc(files, func(file *ent.File) bool { return file.AccountID != domain.LocalAccountID }) {
		exported, err := export.ExportedVideos(root.FS(), directory, record.Code)
		if err != nil {
			return nil, err
		}
		for path, part := range exported {
			file, err := root.Open(path)
			if err != nil {
				return nil, err
			}
			body, err := io.ReadAll(io.LimitReader(file, 8192))
			file.Close()
			if err != nil {
				return nil, err
			}
			if id := export.ParseSTRMFileID(string(body)); id != "" {
				byID[id] = append(byID[id], exportedVideo{path, part})
			}
		}
	}
	var local []pan.File
	for _, file := range files {
		if file.AccountID == domain.LocalAccountID && file.Path != "" {
			local = append(local, pan.File{ID: file.FileID, ParentID: filepath.Dir(file.Path), Name: file.Name})
		}
	}
	plan, planErr := export.PlanVideos(record.Code, local)
	localParts := make(map[string]int, len(plan))
	for _, video := range plan {
		localParts[video.File.ID] = video.Part
	}
	videos := make([]SubtitleVideo, 0, len(files))
	for _, file := range files {
		path, part := file.Path, localParts[file.FileID]
		if planErr != nil && file.AccountID == domain.LocalAccountID {
			// An ambiguous multipart layout can still accept explicit uploads.
			part = -1
		}
		if part == 0 && domain.IsSTRM(file.Name) {
			part, _ = export.VideoPart(filepath.Base(directory), file.Name)
		}
		if file.AccountID != domain.LocalAccountID {
			matches := byID[file.FileID]
			if len(matches) != 1 {
				continue
			}
			path, part = matches[0].path, matches[0].part
		}
		if !filepath.IsLocal(path) || !domain.IsMedia(filepath.Base(path)) {
			continue
		}
		info, err := root.Lstat(path)
		if os.IsNotExist(err) || err == nil && !info.Mode().IsRegular() {
			continue
		}
		if err != nil {
			return nil, err
		}
		version := subtitlemeta.DetectVersion(file.Name)
		target := subtitlemeta.Target{
			Code: record.Code, Dir: filepath.Dir(path), Stem: mediaStem(path), Version: version,
			Uncensored: subtitlemeta.IsUncensored(file.Name), HardSubtitled: subtitlemeta.HasHardSubtitle(file.Name),
		}
		videos = append(videos, SubtitleVideo{
			ID: file.ID, Name: file.Name, CanDownload: part == 0 && s.subtitles != nil, HardSubtitled: target.HardSubtitled,
			Tracks: []SubtitleTrack{}, rootDir: root.Name(), path: path, target: target,
		})
	}
	slices.SortFunc(videos, func(a, b SubtitleVideo) int { return strings.Compare(a.path, b.path) })
	return videos, s.loadSubtitleTracks(ctx, root, configuredDir, record.ID, videos)
}

func (s *Service) loadSubtitleTracks(ctx context.Context, root *os.Root, configuredDir string, id int, videos []SubtitleVideo) error {
	tracks, err := s.database.Subtitle.Query().Where(subtitle.MovieIDEQ(id)).Order(ent.Asc(subtitle.FieldID)).All(ctx)
	if err != nil {
		return err
	}
	byPath := make(map[string]*ent.Subtitle, len(tracks))
	for _, track := range tracks {
		if relative := subtitleRelativePath(root.Name(), configuredDir, track.StoragePath); relative != "" {
			byPath[relative] = track
		}
	}
	for i := range videos {
		video := &videos[i]
		entries, err := fs.ReadDir(root.FS(), filepath.ToSlash(video.target.Dir))
		if err != nil {
			return err
		}
		for _, entry := range entries {
			name := entry.Name()
			format := subtitlemeta.Format(filepath.Ext(name))
			if !entry.Type().IsRegular() || format == "" {
				continue
			}
			path := filepath.Join(video.target.Dir, name)
			saved := byPath[path]
			if !matchesSubtitle(video.target.Stem, name, entries, saved != nil) {
				continue
			}
			info, err := entry.Info()
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return err
			}
			key := fmt.Sprintf("%s\x00%d\x00%d", path, info.ModTime().UnixNano(), info.Size())
			language := subtitleFileLanguage(name)
			track := SubtitleTrack{ID: fmt.Sprintf("%x", sha256.Sum256([]byte(key))), Name: name,
				Format: format, Language: language, Source: "local"}
			if saved != nil {
				track.recordID = saved.ID
				track.Source = saved.Source
				if language == "" {
					track.Language = saved.Language
				}
			}
			video.Tracks = append(video.Tracks, track)
		}
	}
	return nil
}

func subtitleFileLanguage(name string) string {
	for _, part := range strings.Split(strings.ToLower(mediaStem(name)), ".") {
		switch part {
		case "en", "eng":
			return "en"
		case "ja", "jpn":
			return "ja"
		case "ko", "kor":
			return "ko"
		}
	}
	return string(subtitlemeta.LanguageHint(name))
}

func matchesSubtitle(stem, name string, entries []os.DirEntry, indexed bool) bool {
	name, stem = strings.ToLower(mediaStem(name)), strings.ToLower(stem)
	matched := name == stem || strings.HasPrefix(name, stem+".")
	mediaCount := 0
	for _, entry := range entries {
		if entry.Type().IsRegular() && domain.IsMedia(entry.Name()) {
			mediaCount++
			other := strings.ToLower(mediaStem(entry.Name()))
			if len(other) > len(stem) && (name == other || strings.HasPrefix(name, other+".")) {
				return false
			}
		}
	}
	return matched || indexed && mediaCount == 1
}

func mediaStem(path string) string {
	name := filepath.Base(path)
	return strings.TrimSuffix(name, filepath.Ext(name))
}

func subtitleRelativePath(dir, configuredDir, path string) string {
	if path == "" {
		return ""
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	configuredDir, _ = filepath.Abs(configuredDir)
	for _, base := range []string{dir, configuredDir} {
		if relative, err := filepath.Rel(base, path); err == nil && filepath.IsLocal(relative) {
			return relative
		}
	}
	return ""
}
