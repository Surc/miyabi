package subtitle

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/domain"
	subtitlemeta "github.com/ppxb/miyabi/internal/domain/subtitle"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/setting"
	"github.com/ppxb/miyabi/internal/ent/subtitle"
	"github.com/ppxb/miyabi/internal/export"
)

const pendingSubtitlePrefix = "subtitle.pending.v1."

type pendingSubtitle struct {
	Version      int                 `json:"version"`
	MovieID      int                 `json:"movie_id"`
	Root         string              `json:"root"`
	RootIdentity string              `json:"root_identity,omitempty"`
	Target       subtitlemeta.Target `json:"target"`
	Track        subtitlemeta.Track  `json:"track"`
	Hash         string              `json:"sha256"`
	Body         []byte              `json:"body"`
}

func subtitleDigest(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
func pendingKey(target subtitlemeta.Target, track subtitlemeta.Track) string {
	return pendingSubtitlePrefix + subtitleDigest([]byte(filepath.Clean(target.Path(track.Kind))))
}
func recoveryConflict() error {
	return domain.E(domain.KindConflict, "字幕恢复目标或内容已变化，已保留文件和恢复记录，请检查后重试", nil)
}

// Recover replays durable handoffs without searching or downloading. Call before
// starting writers; Export also recovers its movie's handoffs on every retry.
func (s *Service) Recover(ctx context.Context) error { return s.recoverPending(ctx, 0) }
func (s *Service) recoverPending(ctx context.Context, movieID int) error {
	return s.exports.WithConfig(func(cfg export.Config) error { return s.recoverLocked(ctx, cfg, movieID) })
}

// RecoverLocked is the export manager's before-write hook. It must not acquire
// the manager lock again; all export writers wait for these handoffs to settle.
func (s *Service) RecoverLocked(ctx context.Context, cfg export.Config) error {
	return s.recoverLocked(ctx, cfg, 0)
}
func (s *Service) recoverLocked(ctx context.Context, cfg export.Config, movieID int) error {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	rows, err := s.db.Setting.Query().Where(setting.KeyHasPrefix(pendingSubtitlePrefix)).All(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		var pending pendingSubtitle
		if json.Unmarshal(row.Value, &pending) != nil {
			return recoveryConflict()
		}
		if movieID != 0 && pending.MovieID != movieID {
			continue
		}
		if pending.Version != 1 || pending.MovieID <= 0 || len(pending.Body) == 0 || len(pending.Body) > subtitlemeta.MaxFileSize || pending.Hash != subtitleDigest(pending.Body) || row.Key != pendingKey(pending.Target, pending.Track) {
			return recoveryConflict()
		}
		if err := s.finishPending(ctx, cfg, row.Key, pending); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) saveTrack(ctx context.Context, movieID int, target subtitlemeta.Target, track subtitlemeta.Track, body []byte) (bool, error) {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if len(body) == 0 || len(body) > subtitlemeta.MaxFileSize {
		return false, recoveryConflict()
	}
	cfg := s.exports.Config()
	pending := pendingSubtitle{Version: 1, MovieID: movieID, Root: cfg.EmbyDir, Target: target, Track: track, Hash: subtitleDigest(body), Body: body}
	if s.exports != nil && s.exports.PublicationDirectory() != "" {
		identity, err := export.EnsurePublicationRoot(s.exports.PublicationDirectory(), cfg.EmbyDir)
		if err != nil {
			return false, err
		}
		pending.RootIdentity = identity
	}
	if s.exports == nil {
		if err := os.MkdirAll(target.Dir, 0755); err != nil {
			return false, err
		}
	}
	root, err := s.openPendingTarget(ctx, cfg, pending)
	if err != nil {
		return false, err
	}
	name := filepath.Base(target.Path(track.Kind))
	_, err = root.Lstat(name)
	_ = root.Close()
	if err == nil {
		return false, nil
	} // Unowned files never become online records by name.
	if !os.IsNotExist(err) {
		return false, err
	}
	value, err := json.Marshal(pending)
	if err != nil {
		return false, err
	}
	key := pendingKey(target, track)
	if err := s.db.Setting.Create().SetKey(key).SetValue(value).Exec(ctx); err != nil {
		return false, err
	}
	// A failed/ambiguous commit must leave both file and intent intact. The
	// registration transaction removes the marker only with the matching row.
	if err := s.finishPending(ctx, cfg, key, pending); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) openPendingTarget(ctx context.Context, cfg export.Config, p pendingSubtitle) (*os.Root, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	film, err := s.db.Movie.Get(ctx, p.MovieID)
	if err != nil {
		return nil, err
	}
	if film.Code != p.Target.Code || p.Target.Stem == "" || filepath.Base(p.Target.Stem) != p.Target.Stem || strings.ContainsAny(p.Target.Stem, `/\\`) || (p.Target.Version != "" && p.Target.Version != p.Track.Version) {
		return nil, recoveryConflict()
	}
	switch p.Track.Format {
	case "srt", "ass", "ssa", "vtt":
	default:
		return nil, recoveryConflict()
	}
	switch p.Track.Language {
	case "zh-CN", "zh-TW", "en", "ja", "ko":
	default:
		return nil, recoveryConflict()
	}
	switch p.Track.Version {
	case "", "standard", "uncensored", "extended", "leaked":
	default:
		return nil, recoveryConflict()
	}
	dir, err := filepath.Abs(p.Target.Dir)
	if err != nil {
		return nil, err
	}
	if s.exports != nil {
		if cfg.EmbyDir == "" || cfg.EmbyDir != p.Root || p.Target.FileID == "" {
			return nil, recoveryConflict()
		}
		relative, err := filepath.Rel(export.EmbyMovieDir(cfg.EmbyDir, p.Target.Code), dir)
		if err != nil || !filepath.IsLocal(relative) {
			return nil, recoveryConflict()
		}
	}
	// Reject even within-tree links. OpenRoot additionally prevents traversal
	// outside the opened directory if a parent is swapped after these checks.
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	if resolved != dir {
		return nil, recoveryConflict()
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*os.Root, error) { _ = root.Close(); return nil, err }
	if p.RootIdentity != "" {
		identity, err := os.ReadFile(filepath.Join(cfg.EmbyDir, ".miyabi-publication-root"))
		if err != nil || strings.TrimSpace(string(identity)) != p.RootIdentity {
			return fail(recoveryConflict())
		}
	}
	if s.exports != nil {
		strm := p.Target.Stem + ".strm"
		info, err := root.Lstat(strm)
		if err != nil || !info.Mode().IsRegular() {
			return fail(recoveryConflict())
		}
		data, err := root.ReadFile(strm)
		if err != nil || export.ParseSTRMFileID(string(data)) != p.Target.FileID {
			return fail(recoveryConflict())
		}
	}
	return root, nil
}

func (s *Service) finishPending(ctx context.Context, cfg export.Config, key string, p pendingSubtitle) error {
	root, err := s.openPendingTarget(ctx, cfg, p)
	if err != nil {
		return err
	}
	defer root.Close()
	name := filepath.Base(p.Target.Path(p.Track.Kind))
	digest, err := subtitleFileHash(root, name)
	if os.IsNotExist(err) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := installSubtitle(root, name, p.Body, s.syncFile); err != nil {
			return err
		}
		digest, err = subtitleFileHash(root, name)
	}
	if err != nil {
		return err
	}
	if digest != p.Hash {
		return recoveryConflict()
	}
	if s.afterSubtitleInstall != nil {
		if err := s.afterSubtitleInstall(); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Revalidate the visible target after installation: an external rename must
	// not register a path that no longer names the opened directory.
	verified, err := s.openPendingTarget(ctx, cfg, p)
	if err != nil {
		return err
	}
	openedInfo, openedErr := root.Stat(".")
	visibleInfo, visibleErr := verified.Stat(".")
	_ = verified.Close()
	if openedErr != nil || visibleErr != nil || !os.SameFile(openedInfo, visibleInfo) {
		return recoveryConflict()
	}
	// A matching file may be from a Link whose directory Sync failed before
	// the previous process stopped. Re-establish file and link durability on
	// every replay before removing its only persistent recovery record.
	if err := s.syncInstalledSubtitle(root, name, p.Hash); err != nil {
		return err
	}
	return ent.WithTx(ctx, s.db, func(tx *ent.Tx) error {
		film, err := tx.Movie.Get(ctx, p.MovieID)
		if err != nil {
			return err
		}
		if film.Code != p.Target.Code {
			return recoveryConflict()
		}
		rows, err := tx.Subtitle.Query().Where(subtitle.StoragePathEQ(p.Target.Path(p.Track.Kind))).All(ctx)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if row.MovieID != p.MovieID || trackKind(row) != p.Track.Kind || row.Source != p.Track.Source || row.SourceURL != p.Track.SourceURL {
				return recoveryConflict()
			}
		}
		if len(rows) == 0 {
			if err := database.RecordSubtitle(ctx, tx, p.MovieID, p.Target.Path(p.Track.Kind), p.Track); err != nil {
				return err
			}
		}
		_, err = tx.Setting.Delete().Where(setting.KeyEQ(key)).Exec(ctx)
		return err
	})
}

func subtitleFileHash(root *os.Root, name string) (string, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > subtitlemeta.MaxFileSize {
		return "", recoveryConflict()
	}
	file, err := root.Open(name)
	if err != nil {
		return "", err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return "", recoveryConflict()
	}
	body, err := io.ReadAll(io.LimitReader(file, subtitlemeta.MaxFileSize+1))
	if err != nil {
		return "", err
	}
	if len(body) > subtitlemeta.MaxFileSize {
		return "", recoveryConflict()
	}
	return subtitleDigest(body), nil
}

func installSubtitle(root *os.Root, name string, body []byte, syncFile func(*os.File) error) error {
	temporary := ".miyabi-subtitle-" + rand.Text()
	file, err := root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(temporary) }()
	if _, err := file.Write(body); err != nil {
		_ = file.Close()
		return err
	}
	if err := syncFile(file); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	// Hard-link installation is atomic and never overwrites a concurrently
	// created destination; the private candidate is complete and fsynced first.
	if err := root.Link(temporary, name); err != nil {
		return err
	}
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	if err := syncFile(directory); err != nil {
		return fmt.Errorf("sync subtitle directory: %w", err)
	}
	return nil
}

func (s *Service) syncFile(file *os.File) error {
	if s.syncSubtitleFile != nil {
		return s.syncSubtitleFile(file)
	}
	return file.Sync()
}

func (s *Service) syncInstalledSubtitle(root *os.Root, name, expected string) error {
	info, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > subtitlemeta.MaxFileSize {
		return recoveryConflict()
	}
	file, err := root.Open(name)
	if err != nil {
		return err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return recoveryConflict()
	}
	body, err := io.ReadAll(io.LimitReader(file, subtitlemeta.MaxFileSize+1))
	if err != nil {
		return err
	}
	if len(body) > subtitlemeta.MaxFileSize || subtitleDigest(body) != expected {
		return recoveryConflict()
	}
	if err := s.syncFile(file); err != nil {
		return fmt.Errorf("sync installed subtitle: %w", err)
	}
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	if err := s.syncFile(directory); err != nil {
		return fmt.Errorf("sync installed subtitle directory: %w", err)
	}
	return nil
}
