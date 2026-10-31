package export

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ppxb/miyabi/internal/nfo"
)

// mediaFiles only descends into the single level of folders generated for
// multipart versions. Custom files and symbolic links remain untouched.
func mediaFiles(root, stem string) ([]string, error) {
	var files []string
	var read func(string) error
	read = func(dir string) error {
		entries, err := os.ReadDir(filepath.Join(root, dir))
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

var subtitleEnding = regexp.MustCompile(`^(?:\.(?:uncensored|extended|leaked))?\.zh-(?:CN|TW)\.(?:srt|ass|ssa|vtt)$`)

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
		for _, entry := range entries {
			tail := strings.TrimPrefix(entry.Name(), oldStem)
			if !entry.Type().IsRegular() || !subtitleEnding.MatchString(tail) {
				continue
			}
			path := filepath.Join(root, filepath.Dir(old), entry.Name())
			// A full-movie subtitle must never be attached to a video fragment.
			if target != nil && target.Part == 0 {
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
