package scan

import (
	"context"
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
	"github.com/ppxb/miyabi/internal/ent/subtitle"
	"github.com/ppxb/miyabi/internal/export"
	mediaimage "github.com/ppxb/miyabi/internal/image"
	"github.com/ppxb/miyabi/internal/tasks"
)

// LocalScanResult summarizes the outcome of a local directory scan.
type LocalScanResult struct {
	FilesScanned  int    `json:"files_scanned"`
	MediaFiles    int    `json:"media_files"`
	MoviesAdded   int    `json:"movies_added"`
	NFORead       int    `json:"nfo_read"`
	LastMediaPath string `json:"-"`
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
}

// Scan walks rootDir and imports any found media (.strm, .mp4, etc.) into the SQLite database.
func (s *LocalScanner) Scan(ctx context.Context, rootDir string) (*LocalScanResult, error) {
	return s.ScanWithProgress(ctx, rootDir, LocalScanResult{}, nil)
}

// ScanWithProgress resumes after the last committed media file and reports each
// committed batch after releasing export protection.
func (s *LocalScanner) ScanWithProgress(ctx context.Context, rootDir string, previous LocalScanResult, report func(LocalScanResult) error) (*LocalScanResult, error) {
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
		group := dirs[dir]
		if group == nil {
			group = &dirGroup{}
			dirs[dir] = group
		}
		name := d.Name()
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}

		if domain.IsMedia(name) {
			group.mediaFiles = append(group.mediaFiles, info)
		} else if strings.EqualFold(filepath.Ext(name), ".nfo") {
			group.nfoFiles = append(group.nfoFiles, path)
		} else if isImageFile(name) {
			group.imageFiles = append(group.imageFiles, path)
		} else if domain.IsSubtitle(name) {
			group.subFiles = append(group.subFiles, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk local directory %s: %w", rootDir, err)
	}

	batch := make([]localMedia, 0, localScanBatchSize)
	flush := func() error {
		if err := s.ingestBatch(ctx, rootDir, batch, &result); err != nil {
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
			if !ok || code == "" {
				continue
			}
			posterPath, fanartPath := findMatchingArtwork(dir, stem, group.imageFiles)
			batch = append(batch, localMedia{
				path: mediaPath, name: media.Name(), size: media.Size(), code: code,
				nfoPath:    findMatchingNFO(dir, stem, code, group.nfoFiles),
				posterPath: posterPath, fanartPath: fanartPath,
				subPaths: findMatchingSubtitles(stem, group.subFiles, len(group.mediaFiles) == 1),
			})
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

func isImageFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp"
}

func findMatchingNFO(dir, stem, code string, nfoFiles []string) string {
	candidates := []string{
		filepath.Join(dir, stem+".nfo"),
		filepath.Join(dir, code+".nfo"),
		filepath.Join(dir, "movie.nfo"),
	}
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

func findMatchingArtwork(dir, stem string, imageFiles []string) (posterPath, fanartPath string) {
	posterNames := []string{
		"poster.jpg", "cover.jpg",
		stem + "-poster.jpg", stem + "-cover.jpg", stem + ".jpg",
		"poster.png", "cover.png",
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

// indexLocalSubtitle records a subtitle already beside a local .strm so its
// kind is not exported again.
func indexLocalSubtitle(ctx context.Context, tx *ent.Tx, movieID int, subPath string) error {
	name := filepath.Base(subPath)
	format := subtitlemeta.Format(filepath.Ext(name))
	if format == "" {
		return nil
	}
	exists, err := tx.Subtitle.Query().Where(subtitle.MovieIDEQ(movieID), subtitle.StoragePathEQ(subPath)).Exist(ctx)
	if err != nil || exists {
		return err
	}
	return tx.Subtitle.Create().SetMovieID(movieID).SetName(name).SetFormat(format).
		SetLanguage(string(subtitlemeta.DetectLanguage(name, ""))).SetVersionTag(string(subtitlemeta.DetectVersion(name))).
		SetSource("local").SetStoragePath(subPath).Exec(ctx)
}
