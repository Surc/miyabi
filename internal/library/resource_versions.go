package library

import (
	"context"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/ppxb/miyabi/internal/ent/file"
	"github.com/ppxb/miyabi/internal/magnet"
)

type ResourceVersion struct {
	ID        int      `json:"id"`
	Available bool     `json:"available"`
	Labels    []string `json:"labels"`
	Unmarked  bool     `json:"unmarked"`
}

var leakedVersion = regexp.MustCompile(`(?i)(?:^|[^a-z])leaked(?:[^a-z]|$)`)
var crackedVersion = regexp.MustCompile(`(?i)(?:^|[^a-z])UMR(?:[^a-z]|$)`)

// The catalogue's resource hints describe available magnets, not owned files.
// Keep this read-only extension scoped exactly like the current library list.
func (s *Service) ResourceVersions(ctx context.Context, ids []int) ([]ResourceVersion, error) {
	rows := make([]ResourceVersion, 0, len(ids))
	byID := make(map[int]int, len(ids))
	for _, id := range ids {
		if _, found := byID[id]; found {
			continue
		}
		byID[id] = len(rows)
		rows = append(rows, ResourceVersion{ID: id, Labels: []string{}})
	}
	if len(ids) == 0 {
		return rows, nil
	}
	files, err := s.database.File.Query().Where(libraryScope(s.Source()), file.MovieIDIn(ids...)).
		Select(file.FieldID, file.FieldMovieID, file.FieldName).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range files {
		row := &rows[byID[*item.MovieID]]
		row.Available = true
		label := ownedVersionLabel(item.Name)
		if label == "" {
			row.Unmarked = true
		} else if !slices.Contains(row.Labels, label) {
			row.Labels = append(row.Labels, label)
		}
	}
	for i := range rows {
		slices.Sort(rows[i].Labels)
	}
	return rows, nil
}

func ownedVersionLabel(name string) string {
	name = filepath.Base(name)
	quality := magnet.Infer(name)
	label := ""
	switch {
	case strings.Contains(name, "流出") || leakedVersion.MatchString(name):
		label = "流出"
	case quality.HasCracked || crackedVersion.MatchString(name):
		label = "破解"
	case quality.HasUncensored:
		label = "无码"
	}
	if quality.HasSubtitle {
		label = "中文" + label
	}
	// Combine attributes only within one file. Separate Chinese and cracked
	// copies must never turn into a fictitious Chinese-cracked release.
	return label
}
