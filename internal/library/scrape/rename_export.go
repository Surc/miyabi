package scrape

import (
	"context"
	"strings"

	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/movie"
	"github.com/ppxb/miyabi/internal/export"
)

// Remove files matching the previous movie's generated names after publishing
// the corrected identity. Unrelated files in the old directory remain untouched.
func (service *Service) removePreviousExport(ctx context.Context, root string, record *ent.Movie, code string) (string, error) {
	oldDir, _, err := service.previousExport(ctx, root, record, code)
	if err != nil || oldDir == "" {
		return oldDir, err
	}
	if err := export.RemoveMedia(oldDir, record.MetadataSnapshot.Code); err != nil {
		return "", err
	}
	return oldDir, nil
}

// previousExport makes the deletion decision without changing any file. The
// journal incorporates these exact old outputs into the same recovery plan.
func (service *Service) previousExport(ctx context.Context, root string, record *ent.Movie, code string) (string, string, error) {
	if root == "" || record.MetadataSnapshot == nil || record.MetadataSnapshot.Code == "" {
		return "", "", nil
	}
	oldCode := record.MetadataSnapshot.Code
	oldDir := export.EmbyMovieDir(root, oldCode)
	if strings.EqualFold(oldDir, export.EmbyMovieDir(root, code)) {
		return "", "", nil
	}
	claimed, err := service.db.Movie.Query().Where(movie.CodeEQ(oldCode), movie.IDNEQ(record.ID)).Exist(ctx)
	if err != nil || claimed {
		return "", "", err
	}
	return oldDir, oldCode, nil
}
