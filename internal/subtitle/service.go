package subtitle

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ppxb/miyabi/internal/atomicfile"
	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/domain"
	subtitlemeta "github.com/ppxb/miyabi/internal/domain/subtitle"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/subtitle"
	"github.com/ppxb/miyabi/internal/export"
)

const (
	// MaxTracks bounds online subtitle tracks per video, one of each subtitlemeta.Kind.
	MaxTracks = 3
	// maxDownloads bounds the subtitles downloaded per export: a candidate
	// without a language hint may turn out to repeat an exported kind.
	maxDownloads = 8
)

// Service exports movie subtitles beside their Emby .strm files.
type Service struct {
	db      *ent.Client
	finder  *Finder
	exports *export.Manager
}

func NewService(db *ent.Client, finder *Finder) *Service {
	return &Service{db: db, finder: finder}
}

// SetExportManager coordinates subtitle writes with media publication and local scans.
func (s *Service) SetExportManager(manager *export.Manager) { s.exports = manager }

func (s *Service) withExportTarget(ctx context.Context, target subtitlemeta.Target, run func() error) error {
	return s.exports.WithConfig(func(cfg export.Config) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if s.exports != nil {
			if cfg.EmbyDir == "" || target.FileID == "" {
				return nil
			}
			directory := export.EmbyMovieDir(cfg.EmbyDir, target.Code)
			rel, err := filepath.Rel(directory, target.Dir)
			if err != nil || !filepath.IsLocal(rel) {
				return nil
			}
			body, err := os.ReadFile(filepath.Join(target.Dir, target.Stem+".strm"))
			if os.IsNotExist(err) || err == nil && export.ParseSTRMFileID(string(body)) != target.FileID {
				return nil
			}
			if err != nil {
				return err
			}
		}
		return run()
	})
}

// Export writes a movie's subtitles beside its .strm file and returns how many
// files it wrote. Existing exports are reused; online subtitles fill the
// remaining kinds up to MaxTracks.
func (s *Service) Export(ctx context.Context, movieID int, target subtitlemeta.Target) (int, error) {
	config, err := s.Config(ctx)
	if err != nil {
		return 0, err
	}
	autoDownload := config.Enabled && config.Sources.Any()
	var exported map[subtitlemeta.Kind]bool
	err = s.withExportTarget(ctx, target, func() error {
		var err error
		exported, err = s.reconcileTracks(ctx, movieID, target, autoDownload)
		return err
	})
	if err != nil || exported == nil || !autoDownload || target.HardSubtitled {
		return 0, err
	}
	return s.downloadOnline(ctx, target, exported, config, func(track subtitlemeta.Track, body []byte) (bool, error) {
		var saved bool
		err := s.withExportTarget(ctx, target, func() error {
			destination := target.Path(track.Kind)
			if _, err := os.Lstat(destination); !os.IsNotExist(err) {
				return err
			}
			if err := writeFile(destination, body); err != nil {
				return err
			}
			if err := ent.WithTx(ctx, s.db, func(tx *ent.Tx) error {
				return database.RecordSubtitle(ctx, tx, movieID, destination, track)
			}); err != nil {
				return errors.Join(err, os.Remove(destination))
			}
			saved = true
			return nil
		})
		return saved, err
	})
}

func (s *Service) reconcileTracks(ctx context.Context, movieID int, target subtitlemeta.Target, autoDownload bool) (map[subtitlemeta.Kind]bool, error) {
	tracks, err := s.db.Subtitle.Query().Where(subtitle.MovieIDEQ(movieID)).Order(ent.Asc(subtitle.FieldID)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list movie subtitles: %w", err)
	}
	exported := make(map[subtitlemeta.Kind]bool)
	var pending []*ent.Subtitle
	for _, track := range tracks {
		if target.Version != "" && trackKind(track).Version != target.Version {
			continue
		}
		if !autoDownload && fileExists(track.StoragePath) {
			continue
		}
		destination := target.Path(trackKind(track))
		if track.StoragePath == destination && fileExists(destination) {
			exported[trackKind(track)] = true
		} else if fileExists(track.StoragePath) {
			// This track belongs to another complete version of the movie.
			continue
		} else if fileExists(destination) {
			if exported[trackKind(track)] {
				continue
			}
			// Media export moved this video's subtitle along with its STRM.
			if err := s.db.Subtitle.UpdateOne(track).SetStoragePath(destination).SetName(filepath.Base(destination)).Exec(ctx); err != nil {
				return nil, err
			}
			exported[trackKind(track)] = true
		} else {
			if target.Version != "" && filepath.Base(track.StoragePath) != filepath.Base(destination) {
				continue
			}
			pending = append(pending, track)
		}
	}
	var errs []error
	for _, track := range pending {
		errs = append(errs, s.db.Subtitle.DeleteOne(track).Exec(ctx))
	}
	return exported, errors.Join(errs...)
}

type download struct {
	body     []byte
	language subtitlemeta.Language
	err      error
}

// DownloadMissing uses the configured sources even when automatic downloads are disabled.
func (s *Service) DownloadMissing(ctx context.Context, target subtitlemeta.Target, existing map[subtitlemeta.Kind]bool, save func(subtitlemeta.Track, []byte) (bool, error)) (int, error) {
	config, err := s.Config(ctx)
	if err != nil {
		return 0, err
	}
	if !config.Sources.Any() {
		return 0, domain.E(domain.KindInvalid, "请在设置中至少启用一个字幕源", nil)
	}
	config.Enabled = true
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	return s.downloadOnline(ctx, target, existing, config, save)
}

// downloadOnline fills the remaining kinds in two passes: first a language or
// cut the movie lacks, then another format of one it has.
func (s *Service) downloadOnline(ctx context.Context, target subtitlemeta.Target, exported map[subtitlemeta.Kind]bool, config Config, save func(subtitlemeta.Track, []byte) (bool, error)) (int, error) {
	if len(exported) >= MaxTracks {
		return 0, nil
	}
	candidates, searchErr := s.finder.SearchWithConfig(ctx, target.Code, target.Uncensored, config)
	downloads := make(map[string]download)
	written := make(map[[sha256.Size]byte]bool)
	var downloadErrors []error
	for _, distinctLanguage := range []bool{true, false} {
		for _, candidate := range candidates {
			if err := ctx.Err(); err != nil {
				if len(written) > 0 {
					return len(written), nil
				}
				return 0, domain.E(domain.KindUpstream, "字幕检索或下载超时，请稍后重试", err)
			}
			if target.Version != "" && candidate.Version != target.Version {
				continue
			}
			if len(exported) >= MaxTracks {
				return len(written), nil
			}
			if candidate.Language != subtitlemeta.LangUnknown && !wanted(candidate.Kind(), exported, distinctLanguage) {
				continue
			}
			result, fetched := downloads[candidate.URL]
			if !fetched {
				if len(downloads) >= maxDownloads {
					continue
				}
				result.body, result.language, result.err = s.finder.Download(ctx, candidate)
				downloads[candidate.URL] = result
				if result.err != nil {
					downloadErrors = append(downloadErrors, result.err)
				}
			}
			if result.err != nil {
				continue
			}
			candidate.Language = result.language
			digest := sha256.Sum256(result.body)
			if !wanted(candidate.Kind(), exported, distinctLanguage) || written[digest] {
				continue
			}
			track := subtitlemeta.Track{Kind: candidate.Kind(), Source: candidate.Provider, SourceURL: candidate.URL}
			saved, err := save(track, result.body)
			if err != nil {
				return len(written), err
			}
			exported[candidate.Kind()] = true
			if saved {
				written[digest] = true
			}
		}
	}
	if len(written) == 0 {
		if err := errors.Join(searchErr, errors.Join(downloadErrors...), ctx.Err()); err != nil {
			return 0, domain.E(domain.KindUpstream, "字幕检索或下载失败，请稍后重试", err)
		}
	}
	return len(written), nil
}

// wanted reports whether kind adds a track. With distinctLanguage, the movie
// must also lack any format of the kind's language and cut.
func wanted(kind subtitlemeta.Kind, exported map[subtitlemeta.Kind]bool, distinctLanguage bool) bool {
	if exported[kind] {
		return false
	}
	if distinctLanguage {
		for other := range exported {
			if other.Language == kind.Language && other.Version == kind.Version {
				return false
			}
		}
	}
	return true
}

func trackKind(track *ent.Subtitle) subtitlemeta.Kind {
	return subtitlemeta.Kind{Language: subtitlemeta.Language(track.Language), Version: subtitlemeta.VersionTag(track.VersionTag), Format: subtitlemeta.Format(track.Format)}
}

func fileExists(name string) bool {
	info, err := os.Stat(name)
	return err == nil && !info.IsDir()
}

func writeFile(name string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return fmt.Errorf("create subtitle directory: %w", err)
	}
	if err := atomicfile.WriteFile(name, body, 0o644); err != nil {
		return fmt.Errorf("write subtitle: %w", err)
	}
	return nil
}
