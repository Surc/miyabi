package catalogue

import (
	"archive/zip"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ppxb/miyabi/internal/domain"
)

const (
	BrowseArchiveMaxBytes    = 512 << 20
	browseArchiveMaxExpanded = 2 << 30
	browseArchiveMaxRecord   = 64 << 20
	browseArchiveFormat      = "miyabi-browse-cache"
)

type browseArchiveManifest struct {
	Format  string `json:"format"`
	Version int    `json:"version"`
}

// ExportCache snapshots only portable catalogue data, never settings or credentials.
func (s *BrowseExtension) ExportCache(ctx context.Context, dst io.Writer) error {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	archive := zip.NewWriter(dst)
	manifest, err := archive.Create("manifest.json")
	if err != nil {
		return err
	}
	if err := json.NewEncoder(manifest).Encode(browseArchiveManifest{browseArchiveFormat, 1}); err != nil {
		return err
	}
	for _, namespace := range browseNamespaces {
		entries, err := os.ReadDir(filepath.Join(s.store.directory, namespace))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !entry.Type().IsRegular() || !browseArchiveName(namespace+"/"+entry.Name()) {
				continue
			}
			file, err := os.Open(filepath.Join(s.store.directory, namespace, entry.Name()))
			if err != nil {
				return err
			}
			target, err := archive.Create(namespace + "/" + entry.Name())
			if err == nil {
				_, err = io.Copy(target, file)
			}
			file.Close()
			if err != nil {
				return err
			}
		}
	}
	return archive.Close()
}

func browseArchiveName(name string) bool {
	parts := strings.Split(name, "/")
	if len(parts) != 2 || !slices.Contains(browseNamespaces, parts[0]) || len(parts[1]) != 69 || !strings.HasSuffix(parts[1], ".json") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimSuffix(parts[1], ".json"))
	return err == nil
}

func readBrowseArchiveFile(file *zip.File, limit int64) ([]byte, error) {
	if file.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("cache entry exceeds size limit")
	}
	source, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer source.Close()
	data, err := io.ReadAll(io.LimitReader(source, limit+1))
	if err == nil && int64(len(data)) > limit {
		err = fmt.Errorf("cache entry exceeds size limit")
	}
	return data, err
}

func validateBrowseRecord(namespace string, data []byte) (browseRecord, error) {
	var record browseRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return record, err
	}
	if record.UpdatedAt.IsZero() || len(record.Data) == 0 {
		return record, fmt.Errorf("invalid cache record")
	}
	if string(record.Data) == "null" && namespace != "indexes" && namespace != "taxonomy" {
		return record, fmt.Errorf("invalid cache record")
	}
	var value any
	switch namespace {
	case "works":
		value = &domain.MovieDetail{}
	case "references":
		value = &browseReference{}
	case "indexes":
		value = &[]Movie{}
	case "resources":
		value = &BrowseResources{}
	case "ratings":
		value = &domain.ExternalRating{}
	case "images":
		value = &domain.Media{}
	case "taxonomy":
		value = &[]domain.TagCategory{}
	}
	if err := json.Unmarshal(record.Data, value); err != nil {
		return record, err
	}
	valid := true
	switch value := value.(type) {
	case *domain.MovieDetail:
		valid = value.ID != ""
	case *BrowseResources:
		valid = value.Movie.ID != "" && value.Complete
	case *domain.ExternalRating:
		valid = value.Max > 0 && (value.Score == nil || (*value.Score >= 0 && *value.Score <= float64(value.Max)))
	case *domain.Media:
		valid = strings.HasPrefix(value.ContentType, "image/") && len(value.Body) > 0
	}
	if !valid {
		return record, fmt.Errorf("invalid cache data")
	}
	return record, nil
}

// ImportCache validates and stages the entire archive before merging. Timestamps
// survive a move to another server; newer local records always win.
func (s *BrowseExtension) ImportCache(ctx context.Context, src io.ReaderAt, size int64) (BrowseCacheStats, error) {
	invalid := func(err error) (BrowseCacheStats, error) {
		return BrowseCacheStats{}, domain.E(domain.KindInvalid, "无效的缓存文件，请选择本应用导出的 ZIP 文件（最大 512 MB）", err)
	}
	if size <= 0 || size > BrowseArchiveMaxBytes {
		return invalid(fmt.Errorf("archive exceeds size limit"))
	}
	archive, err := zip.NewReader(src, size)
	if err != nil {
		return invalid(err)
	}
	if len(archive.File) > 100_000 {
		return invalid(fmt.Errorf("too many cache entries"))
	}
	stage, err := os.MkdirTemp(s.store.directory, ".import-*")
	if err != nil {
		return BrowseCacheStats{}, err
	}
	defer os.RemoveAll(stage)
	seen := make(map[string]bool)
	var expanded uint64
	for _, file := range archive.File {
		if err := ctx.Err(); err != nil {
			return BrowseCacheStats{}, err
		}
		if seen[file.Name] || !file.Mode().IsRegular() {
			return invalid(fmt.Errorf("duplicate or non-regular cache entry"))
		}
		seen[file.Name] = true
		limit := int64(browseArchiveMaxRecord)
		if file.Name == "manifest.json" {
			limit = 1024
		} else if !browseArchiveName(file.Name) {
			return invalid(fmt.Errorf("unsupported cache path"))
		}
		if file.UncompressedSize64 > uint64(limit) || file.UncompressedSize64 > browseArchiveMaxExpanded-expanded {
			return invalid(fmt.Errorf("expanded archive exceeds size limit"))
		}
		expanded += file.UncompressedSize64
		data, err := readBrowseArchiveFile(file, limit)
		if err != nil {
			return invalid(err)
		}
		if file.Name == "manifest.json" {
			var manifest browseArchiveManifest
			if err := json.Unmarshal(data, &manifest); err != nil {
				return invalid(err)
			}
			if manifest.Format != browseArchiveFormat || manifest.Version != 1 {
				return invalid(fmt.Errorf("unsupported archive version"))
			}
			continue
		}
		if _, err := validateBrowseRecord(strings.Split(file.Name, "/")[0], data); err != nil {
			return invalid(err)
		}
		path := filepath.Join(stage, filepath.FromSlash(file.Name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return BrowseCacheStats{}, err
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return BrowseCacheStats{}, err
		}
	}
	if !seen["manifest.json"] {
		return invalid(fmt.Errorf("missing archive manifest"))
	}
	if err := ctx.Err(); err != nil {
		return BrowseCacheStats{}, err
	}
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	s.store.generation++
	s.loads.reset()
	for _, file := range archive.File {
		if file.Name == "manifest.json" {
			continue
		}
		source := filepath.Join(stage, filepath.FromSlash(file.Name))
		destination := filepath.Join(s.store.directory, filepath.FromSlash(file.Name))
		data, err := os.ReadFile(source)
		if err != nil {
			return BrowseCacheStats{}, err
		}
		var incoming, existing browseRecord
		_ = json.Unmarshal(data, &incoming)
		if local, err := os.ReadFile(destination); err == nil && json.Unmarshal(local, &existing) == nil && !incoming.UpdatedAt.After(existing.UpdatedAt) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return BrowseCacheStats{}, err
		}
		if err := os.Rename(source, destination); err != nil {
			return BrowseCacheStats{}, err
		}
	}
	return s.store.statsLocked()
}
