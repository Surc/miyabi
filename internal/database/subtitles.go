package database

import (
	"context"
	"path/filepath"

	subtitlemeta "github.com/ppxb/miyabi/internal/domain/subtitle"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/subtitle"
)

// RecordSubtitle stores a track at its current export path.
func RecordSubtitle(ctx context.Context, tx *ent.Tx, movieID int, path string, track subtitlemeta.Track) error {
	if _, err := tx.Subtitle.Delete().Where(subtitle.MovieIDEQ(movieID), subtitle.StoragePathEQ(path)).Exec(ctx); err != nil {
		return err
	}
	return tx.Subtitle.Create().SetMovieID(movieID).SetName(filepath.Base(path)).
		SetLanguage(string(track.Language)).SetFormat(track.Format).SetVersionTag(string(track.Version)).
		SetSource(track.Source).SetSourceURL(track.SourceURL).SetStoragePath(path).Exec(ctx)
}
