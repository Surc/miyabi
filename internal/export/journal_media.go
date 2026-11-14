package export

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ppxb/miyabi/internal/nfo"
	"github.com/ppxb/miyabi/internal/pan"
)

// publicationMedia copies only known generated sidecars and explicitly owned
// subtitles, never source videos or arbitrary user files from the directory.
func publicationMedia(root, code string, subtitlePaths map[string]bool) (map[string]publicationFile, error) {
	result := make(map[string]publicationFile)
	confined, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer confined.Close()
	read := func(path string) (publicationFile, error) {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return publicationFile{}, err
		}
		return rootRead(confined, rel)
	}
	directory := EmbyMovieDir(root, code)
	if err := checkAbsolutePath(directory, true); err != nil {
		return nil, err
	}
	stem := nfo.FileStem(code)
	var visit func(string) error
	visit = func(dir string) error {
		relative, err := filepath.Rel(root, dir)
		if err != nil {
			return err
		}
		if err := checkRootPath(confined, relative, true); err != nil {
			return err
		}
		entries, err := fs.ReadDir(confined.FS(), relative)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		var candidates []string
		var videos []string
		owned := false
		for _, entry := range entries {
			name := entry.Name()
			path := filepath.Join(dir, name)
			if dir == directory && VersionDirectory(stem, name) {
				if entry.Type()&os.ModeSymlink != 0 {
					return fmt.Errorf("publication refuses symbolic version directory: %s", path)
				}
				if entry.IsDir() {
					if err := visit(path); err != nil {
						return err
					}
					continue
				}
			}
			_, video := VideoPart(stem, name)
			generated := video || mediaNFO(stem, name) || name == "poster.jpg" || name == "fanart.jpg"
			if !generated {
				continue
			}
			if !entry.Type().IsRegular() {
				return fmt.Errorf("publication target is not a regular file: %s", path)
			}
			file, err := read(path)
			if err != nil {
				return err
			}
			if video && ParseSTRMFileID(string(file.body)) != "" {
				owned = true
				videos = append(videos, strings.TrimSuffix(name, ".strm"))
			}
			if mediaNFO(stem, name) {
				if doc, err := nfo.Decode(file.body); err == nil && strings.EqualFold(doc.Code, code) {
					owned = true
				}
			}
			candidates = append(candidates, path)
		}
		if !owned {
			return nil
		}
		for _, path := range candidates {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			file, err := read(path)
			if err != nil {
				return err
			}
			result[rel] = file
		}
		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())
			if !subtitlePaths[path] {
				continue
			}
			matches := false
			for _, video := range videos {
				if strings.HasPrefix(entry.Name(), video) && subtitleEnding.MatchString(strings.TrimPrefix(entry.Name(), video)) {
					matches = true
					break
				}
			}
			if !matches {
				continue
			}
			if !entry.Type().IsRegular() {
				return fmt.Errorf("publication subtitle is not a regular file: %s", path)
			}
			file, err := read(path)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			result[rel] = file
		}
		return nil
	}
	if err := visit(directory); err != nil {
		return nil, err
	}
	return result, nil
}

// Compute moves using exact STRM identity and part matching, not equal subtitle
// content alone. These changes are consumed by the same SQL publication tx.
func (p *Publication) subtitleChanges(code string, videos []pan.File, owned map[string]bool, before, after map[string]publicationFile) error {
	plan, err := PlanVideos(code, videos)
	if err != nil {
		return err
	}
	directory := EmbyMovieDir(p.Root, code)
	stem := nfo.FileStem(code)
	for oldPath := range owned {
		rel, err := filepath.Rel(p.Root, oldPath)
		if err != nil {
			return err
		}
		old, wasCopied := before[rel]
		if !wasCopied {
			continue
		}
		if _, retained := after[rel]; retained {
			continue
		}
		matched := false
		for strmPath, strm := range before {
			part, ok := VideoPart(stem, filepath.Base(strmPath))
			if !ok || filepath.Dir(rel) != filepath.Dir(strmPath) {
				continue
			}
			oldStem := strings.TrimSuffix(filepath.Base(strmPath), ".strm")
			tail := strings.TrimPrefix(filepath.Base(rel), oldStem)
			if !subtitleEnding.MatchString(tail) {
				continue
			}
			fileID := ParseSTRMFileID(string(strm.body))
			for _, target := range plan {
				if fileID == "" || target.File.ID != fileID || target.Part != part {
					continue
				}
				newPath := filepath.Join(directory, target.Dir, target.Stem+tail)
				newRel, err := filepath.Rel(p.Root, newPath)
				if err != nil {
					return err
				}
				next, exists := after[newRel]
				if !exists || !bytes.Equal(old.body, next.body) {
					return fmt.Errorf("subtitle move plan does not retain its bytes: %s", rel)
				}
				if p.SubtitleMoves == nil {
					p.SubtitleMoves = make(map[string]string)
				}
				p.SubtitleMoves[oldPath] = newPath
				matched = true
				break
			}
			if matched {
				break
			}
		}
		if !matched {
			p.SubtitleDeletes = append(p.SubtitleDeletes, oldPath)
		}
	}
	slices.Sort(p.SubtitleDeletes)
	return nil
}
