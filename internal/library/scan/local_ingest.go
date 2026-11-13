package scan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/ppxb/miyabi/internal/codeid"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/file"
	"github.com/ppxb/miyabi/internal/ent/localscanentry"
	"github.com/ppxb/miyabi/internal/ent/movie"
	"github.com/ppxb/miyabi/internal/export"
	mediaimage "github.com/ppxb/miyabi/internal/image"
	"github.com/ppxb/miyabi/internal/library/scrape"
	"github.com/ppxb/miyabi/internal/nfo"
	"github.com/ppxb/miyabi/internal/tasks"
)

const localScanBatchSize = 32

type localMedia struct {
	path, name, code                string
	size                            int64
	nfoPath, posterPath, fanartPath string
	subPaths                        []string
	info                            map[string]fs.FileInfo
	rel, fileID                     string
	fingerprint                     string
	importIncomplete                bool
	document                        *nfo.Movie
	artwork                         mediaimage.Artwork
}

// Prepare sidecars and artwork before taking SQLite's writer lock. Only one
// bounded batch is prepared at a time; decoded images are not kept in memory.
func (s *LocalScanner) prepareMedia(ctx context.Context, media *localMedia, cfg export.Config) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if media.nfoPath != "" {
		body, err := os.ReadFile(media.nfoPath)
		if err != nil {
			media.importIncomplete = true
			slog.WarnContext(ctx, "failed to read local NFO", "path", media.nfoPath, "error", err)
		} else if doc, err := nfo.Decode(body); err != nil {
			slog.WarnContext(ctx, "failed to decode local NFO", "path", media.nfoPath, "error", err)
		} else {
			doc.Code = codeid.Normalize(doc.Code)
			if doc.Code == "" {
				doc.Code = media.code
			}
			media.document = &doc
		}
	}
	if s.images != nil && media.posterPath != "" {
		poster, err := os.ReadFile(media.posterPath)
		media.importIncomplete = media.importIncomplete || err != nil
		if len(poster) > 0 {
			var fanart []byte
			if media.fanartPath != "" {
				fanart, err = os.ReadFile(media.fanartPath)
				media.importIncomplete = media.importIncomplete || err != nil
			}
			var artwork mediaimage.Artwork
			if len(fanart) > 0 {
				artwork, err = s.images.Restore(poster, fanart)
			} else {
				artwork, err = s.images.Restore(poster, poster)
			}
			if err == nil {
				media.artwork = artwork
			} else {
				media.importIncomplete = true
			}
		}
	}
	if domain.IsSTRM(media.name) {
		info, err := os.Stat(media.path)
		if err != nil {
			return err
		}
		fileID, rewritten, err := export.RewriteSTRMFile(media.path, cfg)
		if err != nil {
			return err
		}
		media.fileID = fileID
		if rewritten {
			info, err = os.Stat(media.path)
			if err != nil {
				return err
			}
		}
		media.size = info.Size()
		media.fingerprint = localFingerprint(*media, info)
	}
	if media.fileID == "" {
		media.fileID = localFileID(media.path)
	}
	return ctx.Err()
}

// The canonical absolute path distinguishes identical names in different libraries.
func localFileID(path string) string {
	hash := sha256.Sum256([]byte(path))
	return "local-" + hex.EncodeToString(hash[:16])
}

func (s *LocalScanner) ingestBatch(ctx context.Context, rootDir string, batch []localMedia, result *LocalScanResult) error {
	if s.exports == nil {
		return s.importBatch(ctx, rootDir, batch, result, export.Config{})
	}
	return s.exports.WithConfig(func(cfg export.Config) error {
		current, err := filepath.Abs(cfg.EmbyDir)
		if err == nil {
			current, err = filepath.EvalSymlinks(current)
		}
		if err != nil {
			return fmt.Errorf("resolve current export directory: %w", err)
		}
		if current != rootDir {
			return domain.E(domain.KindConflict, "Emby 本地目录已变更，请扫描当前目录", nil)
		}
		return s.importBatch(ctx, rootDir, batch, result, cfg)
	})
}

func (s *LocalScanner) importBatch(ctx context.Context, rootDir string, batch []localMedia, result *LocalScanResult, cfg export.Config) error {
	if s.images != nil {
		for _, media := range batch {
			if media.posterPath != "" {
				if err := s.images.LockArtwork(ctx); err != nil {
					return err
				}
				defer s.images.UnlockArtwork()
				break
			}
		}
	}
	if err := tasks.Checkpoint(ctx, s.db); err != nil {
		return err
	}
	codes, fileIDs := make([]string, len(batch)), make([]string, len(batch))
	for i := range batch {
		if err := s.prepareMedia(ctx, &batch[i], cfg); err != nil {
			return fmt.Errorf("prepare %s: %w", batch[i].path, err)
		}
		codes[i], fileIDs[i] = batch[i].code, batch[i].fileID
	}
	var added, nfoRead int
	err := ent.WithTx(ctx, s.db, func(tx *ent.Tx) error {
		records, err := tx.File.Query().Where(file.FileIDIn(fileIDs...)).All(ctx)
		if err != nil {
			return err
		}
		files := make(map[string]*ent.File, len(records))
		var associatedIDs []int
		for _, record := range records {
			files[record.FileID] = record
			if record.MovieID != nil {
				associatedIDs = append(associatedIDs, *record.MovieID)
			}
		}
		matcher, err := loadMovieMatcher(ctx, tx, codes, associatedIDs...)
		if err != nil {
			return err
		}
		manualMovies := make(map[int]bool)
		for _, group := range matcher {
			for _, record := range group {
				manualMovies[record.ID] = record.ManualCode != ""
			}
		}
		directories := make(map[string]bool)
		entries := make([]*ent.LocalScanEntryCreate, 0, len(batch))
		remember := func(media localMedia, id int) {
			if !media.importIncomplete {
				entries = append(entries, tx.LocalScanEntry.Create().SetRoot(rootDir).
					SetPath(media.rel).SetFingerprint(media.fingerprint).SetFileID(id))
			}
		}
		for _, media := range batch {
			if err := ctx.Err(); err != nil {
				return err
			}
			existing := files[media.fileID]
			if existing != nil && existing.AccountID == domain.LocalAccountID &&
				(existing.Name != media.name || existing.Size != media.size || existing.Path != media.rel) {
				updated, err := tx.File.UpdateOneID(existing.ID).SetName(media.name).SetSize(media.size).SetPath(media.rel).Save(ctx)
				if err != nil {
					return err
				}
				existing, files[media.fileID] = updated, updated
			}
			// A stale exported NFO must not undo a manual correction or reassign its files.
			if existing != nil && existing.MovieID != nil && manualMovies[*existing.MovieID] {
				remember(media, existing.ID)
				continue
			}
			record := matcher.find(media.code)
			if record == nil {
				record, err = tx.Movie.Create().SetCode(media.code).Save(ctx)
				if err != nil {
					return err
				}
				matcher.add(record)
				added++
			}
			// An old exported NFO does not mean a failed refresh has recovered.
			if media.document != nil && record.ScrapeStatus != movie.ScrapeStatusFailed {
				nfoRead++
				var current *ent.Movie
				if err := scrape.SaveMovieMetadata(ctx, tx, record.ID, *media.document); err != nil {
					media.importIncomplete = true
					slog.WarnContext(ctx, "failed to save movie metadata from local NFO", "path", media.nfoPath, "error", err)
					// Even a rejected NFO may have changed fields before a later write failed.
					current, err = tx.Movie.Get(ctx, record.ID)
					if err != nil {
						return err
					}
				} else {
					current, err = tx.Movie.UpdateOneID(record.ID).SetScrapeStatus(movie.ScrapeStatusDone).Save(ctx)
					if err != nil {
						return err
					}
				}
				matcher.remove(record)
				matcher.add(current)
				record = current
			}
			if media.artwork != (mediaimage.Artwork{}) {
				if err := tx.Movie.UpdateOneID(record.ID).SetPoster(media.artwork.Poster).
					SetCover(media.artwork.Thumbnail).SetFanarts([]string{media.artwork.Fanart}).Exec(ctx); err != nil {
					return err
				}
			}
			if existing == nil {
				created, err := tx.File.Create().SetFileID(media.fileID).SetName(media.name).SetSize(media.size).
					SetAccountID(domain.LocalAccountID).SetRootID(domain.LocalAccountID).
					SetPath(media.rel).SetMovieID(record.ID).Save(ctx)
				if err != nil {
					return err
				}
				files[media.fileID] = created
			} else if existing.MovieID == nil || *existing.MovieID != record.ID {
				updated, err := tx.File.UpdateOneID(existing.ID).SetMovieID(record.ID).Save(ctx)
				if err != nil {
					return err
				}
				files[media.fileID] = updated
			}
			for _, subPath := range media.subPaths {
				if err := indexLocalSubtitle(ctx, tx, record.ID, subPath); err != nil {
					return err
				}
			}
			remember(media, files[media.fileID].ID)
			directories[filepath.Dir(media.path)] = true
		}
		if len(entries) > 0 {
			if err := tx.LocalScanEntry.CreateBulk(entries...).OnConflictColumns(localscanentry.FieldRoot, localscanentry.FieldPath).
				UpdateNewValues().UpdateFileID().Exec(ctx); err != nil {
				return fmt.Errorf("save local scan index: %w", err)
			}
		}
		if s.notifier != nil {
			for dir := range directories {
				if err := s.notifier.NotifyUpdatedTx(ctx, tx, dir); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("ingest local batch starting at %s: %w", batch[0].path, err)
	}
	result.MoviesAdded += added
	result.NFORead += nfoRead
	return nil
}
