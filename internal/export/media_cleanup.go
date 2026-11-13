package export

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ppxb/miyabi/internal/nfo"
)

// mediaFiles only descends into the single level of folders generated for
// multipart versions. Custom files and symbolic links remain untouched.
func mediaFiles(root, stem string) ([]string, error) {
	return readMediaFiles(os.DirFS(root), ".", stem)
}

func readMediaFiles(root fs.FS, directory, stem string) ([]string, error) {
	var files []string
	var read func(string) error
	read = func(dir string) error {
		entries, err := fs.ReadDir(root, path.Join(directory, filepath.ToSlash(dir)))
		if err != nil {
			return err
		}
		for _, entry := range entries {
			name := entry.Name()
			if dir == "" && entry.IsDir() && VersionDirectory(stem, name) {
				if err := read(name); err != nil {
					return err
				}
				continue
			}
			if !entry.Type().IsRegular() {
				continue
			}
			_, video := VideoPart(stem, name)
			if video || mediaNFO(stem, name) || name == "poster.jpg" || name == "fanart.jpg" {
				files = append(files, filepath.Join(dir, name))
			}
		}
		return nil
	}
	err := read("")
	return files, err
}

var subtitleEnding = regexp.MustCompile(`^(?:(?:\.(?:uncensored|extended|leaked))?\.zh-(?:CN|TW)|\.miyabi(?:\.(?:uncensored|extended|leaked))?\.(?:en|ja|ko))\.(?:srt|ass|ssa|vtt)$`)

// ExportedVideos finds existing Miyabi STRMs and their parts, keyed by their path in root.
func ExportedVideos(root fs.FS, directory, code string) (map[string]int, error) {
	stem := nfo.FileStem(code)
	paths, err := readMediaFiles(root, filepath.ToSlash(directory), stem)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	videos := make(map[string]int)
	for _, path := range paths {
		if part, ok := VideoPart(stem, filepath.Base(path)); ok {
			videos[filepath.Join(directory, path)] = part
		}
	}
	return videos, nil
}

func moveVideoSubtitles(root string, previous map[string]string, plan []VideoExport, write func(string, []byte) error, changed *bool) error {
	for old, id := range previous {
		if id == "" {
			continue
		}
		var target *VideoExport
		for i := range plan {
			if plan[i].File.ID == id {
				target = &plan[i]
				break
			}
		}
		if target != nil && old == filepath.Join(target.Dir, target.Stem+".strm") {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(root, filepath.Dir(old)))
		if err != nil {
			return err
		}
		oldStem := strings.TrimSuffix(filepath.Base(old), ".strm")
		oldPart, _ := VideoPart(filepath.Base(root), filepath.Base(old))
		for _, entry := range entries {
			tail := strings.TrimPrefix(entry.Name(), oldStem)
			if !entry.Type().IsRegular() || !subtitleEnding.MatchString(tail) {
				continue
			}
			path := filepath.Join(root, filepath.Dir(old), entry.Name())
			// Carry the subtitle only when the video keeps the same part number.
			if target != nil && target.Part == oldPart {
				body, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				destination := filepath.Join(target.Dir, target.Stem+tail)
				before, err := os.ReadFile(filepath.Join(root, destination))
				if os.IsNotExist(err) {
					if err := write(destination, body); err != nil {
						return fmt.Errorf("move video subtitle: %w", err)
					}
				} else if err != nil {
					return err
				} else if !bytes.Equal(before, body) {
					return fmt.Errorf("subtitle destination already contains different data: %s", destination)
				}
			}
			if err := os.Remove(path); err != nil {
				return err
			}
			*changed = true
		}
	}
	return nil
}

// RemoveMedia removes the exact generated outputs of an obsolete identity.
func RemoveMedia(root, code string) error {
	stem := nfo.FileStem(code)
	files, err := mediaFiles(root, stem)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, name := range files {
		if _, video := VideoPart(stem, filepath.Base(name)); video {
			entries, err := os.ReadDir(filepath.Join(root, filepath.Dir(name)))
			if err != nil {
				return err
			}
			for _, entry := range entries {
				tail := strings.TrimPrefix(entry.Name(), strings.TrimSuffix(filepath.Base(name), ".strm"))
				if entry.Type().IsRegular() && subtitleEnding.MatchString(tail) {
					if err := os.Remove(filepath.Join(root, filepath.Dir(name), entry.Name())); err != nil {
						return err
					}
				}
			}
		}
		if err := os.Remove(filepath.Join(root, name)); err != nil {
			return err
		}
	}
	return removeEmptyVersionDirs(root, stem)
}

func removeEmptyVersionDirs(root, stem string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !VersionDirectory(stem, entry.Name()) {
			continue
		}
		path := filepath.Join(root, entry.Name())
		children, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		if len(children) == 0 {
			if err := os.Remove(path); err != nil {
				return err
			}
		}
	}
	return nil
}
