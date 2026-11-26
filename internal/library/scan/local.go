package scan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ppxb/miyabi/internal/codeid"
	"github.com/ppxb/miyabi/internal/domain"
	subtitlemeta "github.com/ppxb/miyabi/internal/domain/subtitle"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/file"
	"github.com/ppxb/miyabi/internal/ent/localscanentry"
	"github.com/ppxb/miyabi/internal/ent/subtitle"
	"github.com/ppxb/miyabi/internal/export"
	mediaimage "github.com/ppxb/miyabi/internal/image"
	"github.com/ppxb/miyabi/internal/nfo"
	"github.com/ppxb/miyabi/internal/tasks"
)

// LocalScanResult summarizes the outcome of a local directory scan.
type LocalScanResult struct {
	LastMediaPath   string `json:"last_media_path,omitempty"`
	FilesScanned    int    `json:"files_scanned"`
	MediaFiles      int    `json:"media_files"`
	MoviesAdded     int    `json:"movies_added"`
	NFORead         int    `json:"nfo_read"`
	PendingMovieIDs []int  `json:"-"`
}

func (r *LocalScanResult) addPending(movieID int) {
	for _, existing := range r.PendingMovieIDs {
		if existing == movieID {
			return
		}
	}
	r.PendingMovieIDs = append(r.PendingMovieIDs, movieID)
}

// LocalScanner scans a local directory structure containing .strm and video files.
type LocalScanner struct {
	db       *ent.Client
	images   *mediaimage.Cache
	notifier export.MediaNotifier
	exports  *export.Manager
}

// NewLocalScanner creates a new LocalScanner.
func NewLocalScanner(db *ent.Client, images *mediaimage.Cache) *LocalScanner {
	return &LocalScanner{
		db:     db,
		images: images,
	}
}

// SetMediaNotifier sets the notification handler for discovered media folders.
func (s *LocalScanner) SetMediaNotifier(notifier export.MediaNotifier) {
	s.notifier = notifier
}

// SetExportManager fences each import batch against sidecar writes and STRM rewrites.
func (s *LocalScanner) SetExportManager(manager *export.Manager) { s.exports = manager }

type dirGroup struct {
	mediaFiles []fs.FileInfo
	nfoFiles   []string
	imageFiles []string
	subFiles   []string
	info       map[string]fs.FileInfo
}

// Scan checks file metadata under rootDir and imports new or changed media.
func (s *LocalScanner) Scan(ctx context.Context, rootDir string) (*LocalScanResult, error) {
	return s.ScanWithProgress(ctx, rootDir, s.exports != nil, LocalScanResult{}, nil)
}

// ScanWithProgress resumes after the last committed media file and reports each
// committed batch after releasing export protection.
func (s *LocalScanner) ScanWithProgress(ctx context.Context, rootDir string, fromExport bool, previous LocalScanResult, report func(LocalScanResult) error) (*LocalScanResult, error) {
	if err := tasks.Checkpoint(ctx, s.db); err != nil {
		return nil, err
	}
	rootDir, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve local directory: %w", err)
	}
	rootDir, err = filepath.EvalSymlinks(rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve local directory links: %w", err)
	}
	stat, err := os.Stat(rootDir)
	if err != nil {
		return nil, fmt.Errorf("stat local directory %s: %w", rootDir, err)
	}
	if !stat.IsDir() {
		return nil, fmt.Errorf("local path %s is not a directory", rootDir)
	}

	entries, err := s.db.LocalScanEntry.Query().Where(localscanentry.RootEQ(rootDir),
		localscanentry.HasFileWith(file.HasMovie())).
		Select(localscanentry.FieldPath, localscanentry.FieldFingerprint).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load local scan index: %w", err)
	}
	indexed := make(map[string]string, len(entries))
	for _, entry := range entries {
		indexed[entry.Path] = entry.Fingerprint
	}

	result := previous
	result.FilesScanned = 0
	dirs := make(map[string]*dirGroup)
	const pauseCheckInterval = 500 * time.Millisecond
	nextPauseCheck := time.Now().Add(pauseCheckInterval)

	err = filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if now := time.Now(); !now.Before(nextPauseCheck) {
			if err := tasks.Checkpoint(ctx, s.db); err != nil {
				return err
			}
			nextPauseCheck = now.Add(pauseCheckInterval)
		}
		if d.IsDir() || d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		result.FilesScanned++
		dir := filepath.Dir(path)
		name := d.Name()
		media := domain.IsMedia(name)
		nfo := strings.EqualFold(filepath.Ext(name), ".nfo")
		image := isImageFile(name)
		sub := domain.IsSubtitle(name)
		if !media && !nfo && !image && !sub {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		group := dirs[dir]
		if group == nil {
			group = &dirGroup{info: make(map[string]fs.FileInfo)}
			dirs[dir] = group
		}
		group.info[path] = info
		switch {
		case media:
			group.mediaFiles = append(group.mediaFiles, info)
		case nfo:
			group.nfoFiles = append(group.nfoFiles, path)
		case image:
			group.imageFiles = append(group.imageFiles, path)
		case sub:
			group.subFiles = append(group.subFiles, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk local directory %s: %w", rootDir, err)
	}

	batch := make([]localMedia, 0, localScanBatchSize)
	flush := func() error {
		if err := s.ingestBatch(ctx, rootDir, fromExport, batch, &result); err != nil {
			return err
		}
		result.MediaFiles += len(batch)
		result.LastMediaPath = batch[len(batch)-1].path
		if report != nil {
			return report(result)
		}
		return nil
	}
	var resumeDir, resumeName string
	if previous.LastMediaPath != "" {
		resumeDir, resumeName = filepath.Dir(previous.LastMediaPath), filepath.Base(previous.LastMediaPath)
	}
	// WalkDir supplies names in lexical order within each directory. Sorting the
	// directories makes the saved cursor independent of map iteration order.
	for _, dir := range slices.Sorted(maps.Keys(dirs)) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if dir < resumeDir {
			continue
		}
		group := dirs[dir]
		for _, media := range group.mediaFiles {
			if dir == resumeDir && media.Name() <= resumeName {
				continue
			}
			mediaPath := filepath.Join(dir, media.Name())
			stem := strings.TrimSuffix(media.Name(), filepath.Ext(media.Name()))

			code, ok := codeid.Parse(media.Name())
			if !ok || code == "" {
				code, ok = codeid.Parse(filepath.Base(dir))
			}
			nfoPath := findMatchingNFO(dir, stem, code, group.nfoFiles)
			// A local-NFO library may keep the catalogue number only inside the
			// NFO when neither the file nor its folder names it. Fall back to the
			// matched NFO's code so those movies are not silently skipped.
			if (!ok || code == "") && nfoPath != "" {
				if docCode, err := readNFOCode(nfoPath); err == nil && docCode != "" {
					code, ok = docCode, true
				}
			}
			if !ok || code == "" {
				continue
			}
			posterPath, fanartPath := findMatchingArtwork(dir, stem, group.imageFiles)
			rel, err := filepath.Rel(rootDir, mediaPath)
			if err != nil {
				return nil, fmt.Errorf("compute relative media path: %w", err)
			}
			candidate := localMedia{
				path: mediaPath, name: media.Name(), size: media.Size(), code: code,
				nfoPath:    nfoPath,
				posterPath: posterPath, fanartPath: fanartPath,
				subPaths: findMatchingSubtitles(stem, group.subFiles, len(group.mediaFiles) == 1),
				rel:      rel,
				info:     group.info,
			}
			candidate.fingerprint = localFingerprint(candidate, media)
			if candidate.fingerprint == indexed[rel] {
				continue
			}
			batch = append(batch, candidate)
			if len(batch) == localScanBatchSize {
				if err := flush(); err != nil {
					return nil, err
				}
				batch = batch[:0]
			}
		}
	}

	if len(batch) > 0 {
		if err := flush(); err != nil {
			return nil, err
		}
	}
	return &result, nil
}

func localFingerprint(media localMedia, mediaInfo fs.FileInfo) string {
	hash := sha256.New()
	write := func(path string, entry fs.FileInfo) {
		if entry != nil {
			fmt.Fprintf(hash, "%q %d %d", filepath.Base(path), entry.Size(), entry.ModTime().UnixNano())
		}
		_, _ = hash.Write([]byte{'\n'})
	}
	write(media.path, mediaInfo)
	for _, path := range []string{media.nfoPath, media.posterPath, media.fanartPath} {
		write(path, media.info[path])
	}
	for _, path := range media.subPaths {
		write(path, media.info[path])
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func isImageFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp"
}

func findMatchingNFO(dir, stem, code string, nfoFiles []string) string {
	candidates := []string{filepath.Join(dir, stem+".nfo")}
	if code != "" {
		candidates = append(candidates, filepath.Join(dir, code+".nfo"))
	}
	candidates = append(candidates, filepath.Join(dir, "movie.nfo"))
	for _, cand := range candidates {
		for _, f := range nfoFiles {
			if strings.EqualFold(f, cand) {
				return f
			}
		}
	}
	if len(nfoFiles) == 1 {
		return nfoFiles[0]
	}
	return ""
}

// readNFOCode returns the normalized catalogue number stored inside an NFO file.
func readNFOCode(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	doc, err := nfo.Decode(body)
	if err != nil {
		return "", err
	}
	return codeid.Normalize(doc.Code), nil
}

func findMatchingArtwork(dir, stem string, imageFiles []string) (posterPath, fanartPath string) {
	posterNames := []string{
		"poster.jpg", "cover.jpg",
		stem + "-poster.jpg", stem + "-cover.jpg", stem + ".jpg",
		"poster.png", "cover.png",
		stem + "-poster.png", stem + "-cover.png", stem + ".png",
	}
	fanartNames := []string{
		"fanart.jpg", stem + "-fanart.jpg",
		"fanart.png", stem + "-fanart.png",
	}

	for _, name := range posterNames {
		target := filepath.Join(dir, name)
		for _, f := range imageFiles {
			if strings.EqualFold(f, target) {
				posterPath = f
				break
			}
		}
		if posterPath != "" {
			break
		}
	}

	for _, name := range fanartNames {
		target := filepath.Join(dir, name)
		for _, f := range imageFiles {
			if strings.EqualFold(f, target) {
				fanartPath = f
				break
			}
		}
		if fanartPath != "" {
			break
		}
	}
	return posterPath, fanartPath
}

// findMatchingSubtitles returns the subtitles named after a media file, such
// as IPX-123.zh-CN.srt for IPX-123.strm. A lone subtitle belongs to the lone
// media file in its directory.
func findMatchingSubtitles(stem string, subFiles []string, singleVideo bool) []string {
	var matches []string
	for _, f := range subFiles {
		if strings.HasPrefix(strings.ToLower(filepath.Base(f)), strings.ToLower(stem)+".") {
			matches = append(matches, f)
		}
	}
	if len(matches) == 0 && singleVideo && len(subFiles) == 1 {
		return subFiles
	}
	return matches
}

// indexLocalSubtitle records a subtitle already beside a local .strm. A copy
// exported for a downloaded track of the same kind is not a second track.
func indexLocalSubtitle(ctx context.Context, tx *ent.Tx, movieID int, subPath string) error {
	name := filepath.Base(subPath)
	format := subtitlemeta.Format(filepath.Ext(name))
	if format == "" {
		return nil
	}
	language := string(subtitlemeta.DetectLanguage(name, ""))
	version := string(subtitlemeta.DetectVersion(name))
	exists, err := tx.Subtitle.Query().Where(subtitle.MovieIDEQ(movieID), subtitle.Or(
		subtitle.StoragePathEQ(subPath),
		subtitle.And(subtitle.SourceURLNEQ(""), subtitle.LanguageEQ(language), subtitle.VersionTagEQ(version), subtitle.FormatEQ(format)),
	)).Exist(ctx)
	if err != nil || exists {
		return err
	}
	return tx.Subtitle.Create().SetMovieID(movieID).SetName(name).SetFormat(format).
		SetLanguage(language).SetVersionTag(version).
		SetSource("local").SetStoragePath(subPath).Exec(ctx)
}
