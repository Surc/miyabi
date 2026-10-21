package catalogue

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// A separate, versioned directory keeps discovery caches outside library data.
// Clearing a cache advances the generation so older requests cannot resurrect it.
type browseStore struct {
	mu         sync.Mutex
	directory  string
	generation uint64
}
type browseRecord struct {
	UpdatedAt time.Time       `json:"updated_at"`
	Data      json.RawMessage `json:"data"`
}
type BrowseCacheStats struct {
	Movies int   `json:"movies"`
	Images int   `json:"images"`
	Bytes  int64 `json:"bytes"`
}

var browseNamespaces = []string{"works", "references", "indexes", "resources", "ratings", "images"}

func newBrowseStore(directory string) (*browseStore, error) {
	s := &browseStore{directory: filepath.Join(directory, "extensions", "browse-v1")}
	return s, os.MkdirAll(s.directory, 0o700)
}
func (s *browseStore) path(namespace, key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(s.directory, namespace, hex.EncodeToString(sum[:])+".json")
}
func (s *browseStore) read(namespace, key string) (browseRecord, bool, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var r browseRecord
	data, err := os.ReadFile(s.path(namespace, key))
	ok := err == nil && json.Unmarshal(data, &r) == nil && len(r.Data) > 0
	return r, ok, s.generation
}
func (s *browseStore) write(namespace, key string, r browseRecord, generation uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if generation != s.generation && namespace != "config" {
		return nil
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	path := s.path(namespace, key)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".write-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
func (s *browseStore) stats() (BrowseCacheStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statsLocked()
}
func (s *browseStore) statsLocked() (BrowseCacheStats, error) {
	var stats BrowseCacheStats
	for _, namespace := range browseNamespaces {
		entries, err := os.ReadDir(filepath.Join(s.directory, namespace))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return stats, err
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return stats, err
			}
			stats.Bytes += info.Size()
			if namespace == "works" {
				stats.Movies++
			}
			if namespace == "images" {
				stats.Images++
			}
		}
	}
	return stats, nil
}
func (s *browseStore) clear(scope string) (BrowseCacheStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generation++
	for _, namespace := range browseNamespaces {
		if scope == "images" && namespace != "images" {
			continue
		}
		if scope == "data" && namespace == "images" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(s.directory, namespace)); err != nil {
			return BrowseCacheStats{}, err
		}
	}
	return s.statsLocked()
}
