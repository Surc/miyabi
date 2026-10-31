package export

import (
	"bytes"
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ppxb/miyabi/internal/atomicfile"
	"github.com/ppxb/miyabi/internal/nfo"
	"github.com/ppxb/miyabi/internal/pan"
)

// WriteMedia writes artwork, NFO, then STRM files and reports whether any output changed.
// Compare each output independently so repairing one missing sidecar does not
// touch complete files or trigger unnecessary media-server file events.
func WriteMedia(cfg Config, code string, doc nfo.Movie, videos []pan.File, poster, fanart []byte) (bool, error) {
	if err := cfg.Validate(); err != nil {
		return false, err
	}
	plan, err := PlanVideos(code, videos)
	if err != nil {
		return false, err
	}
	stem := nfo.FileStem(code)
	destDir := EmbyMovieDir(cfg.EmbyDir, code)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return false, fmt.Errorf("create emby directory %s: %w", destDir, err)
	}
	previous, err := mediaFiles(destDir, stem)
	if err != nil {
		return false, err
	}
	oldVideos := make(map[string]string)
	for _, name := range previous {
		if _, ok := VideoPart(stem, filepath.Base(name)); ok {
			body, err := os.ReadFile(filepath.Join(destDir, name))
			if err != nil {
				return false, err
			}
			oldVideos[name] = ParseSTRMFileID(string(body))
		}
	}
	changed := false
	expected := make(map[string]bool)
	write := func(name string, body []byte) error {
		expected[name] = true
		path := filepath.Join(destDir, name)
		before, err := os.ReadFile(path)
		if err == nil && bytes.Equal(before, body) {
			return nil
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := atomicfile.WriteFile(path, body, 0o644); err != nil {
			return err
		}
		changed = true
		return nil
	}

	// Each isolated multipart release needs its own complete set of sidecars.
	directories := map[string]string{}
	for _, video := range plan {
		name := stem
		if video.Dir != "" {
			name = video.Dir
		}
		directories[video.Dir] = name
	}
	if len(plan) == 0 {
		directories[""] = stem
	}

	// 1. Write poster and fanart FIRST
	posterName, fanartName := "poster.jpg", "fanart.jpg"
	for dir := range directories {
		expected[filepath.Join(dir, posterName)] = true
		expected[filepath.Join(dir, fanartName)] = true
		if len(poster) > 0 {
			if err := write(filepath.Join(dir, posterName), poster); err != nil {
				return changed, fmt.Errorf("write poster: %w", err)
			}
		}
		if len(fanart) > 0 {
			if err := write(filepath.Join(dir, fanartName), fanart); err != nil {
				return changed, fmt.Errorf("write fanart: %w", err)
			}
		}
	}

	// 2. Write NFO file SECOND
	doc.Thumbs = []nfo.Thumb{{Aspect: "poster", Path: posterName}}
	doc.Fanart = fanartName
	nfoBody, err := nfo.Encode(doc)
	if err != nil {
		return changed, fmt.Errorf("encode nfo: %w", err)
	}
	for dir, name := range directories {
		if err := write(filepath.Join(dir, name+".nfo"), nfoBody); err != nil {
			return changed, fmt.Errorf("write nfo file: %w", err)
		}
	}
	for _, video := range plan {
		if video.Part == 0 && video.Dir == "" && video.Stem != stem {
			if err := write(video.Stem+".nfo", nfoBody); err != nil {
				return changed, err
			}
		}
	}

	// 3. Write STRM files LAST so media servers (Emby) watching via inotify detect complete assets
	for _, video := range plan {
		strmName := filepath.Join(video.Dir, video.Stem+".strm")
		if err := write(strmName, STRMContent(cfg.PublicURL, video.File.ID, cfg.STRMToken)); err != nil {
			return changed, fmt.Errorf("write strm file: %w", err)
		}
	}

	if err := moveVideoSubtitles(destDir, oldVideos, plan, write, &changed); err != nil {
		return changed, err
	}
	// Delete only exact generated names, after all replacement files exist.
	for _, name := range previous {
		if !expected[name] {
			if err := os.Remove(filepath.Join(destDir, name)); err != nil {
				return changed, err
			}
			changed = true
		}
	}
	if err := removeEmptyVersionDirs(destDir, stem); err != nil {
		return changed, err
	}

	return changed, nil
}

// naturalCompare compares two strings using natural numeric order (e.g. "cd2" < "cd10").
func naturalCompare(a, b string) int {
	aLower, bLower := strings.ToLower(a), strings.ToLower(b)
	i, j := 0, 0
	for i < len(aLower) && j < len(bLower) {
		if isDigit(aLower[i]) && isDigit(bLower[j]) {
			startA, startB := i, j
			for i < len(aLower) && isDigit(aLower[i]) {
				i++
			}
			for j < len(bLower) && isDigit(bLower[j]) {
				j++
			}
			numA := strings.TrimLeft(aLower[startA:i], "0")
			numB := strings.TrimLeft(bLower[startB:j], "0")
			if len(numA) != len(numB) {
				return cmp.Compare(len(numA), len(numB))
			}
			if c := cmp.Compare(numA, numB); c != 0 {
				return c
			}
			continue
		}
		if aLower[i] != bLower[j] {
			return cmp.Compare(aLower[i], bLower[j])
		}
		i++
		j++
	}
	return cmp.Compare(len(aLower)-i, len(bLower)-j)
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

// sortFiles sorts video files by Name using natural sort order.
func sortFiles(videos []pan.File) {
	slices.SortFunc(videos, func(a, b pan.File) int {
		if c := naturalCompare(a.Name, b.Name); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
}
