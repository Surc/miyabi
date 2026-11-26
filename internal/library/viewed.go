package library

import (
	"context"
	"fmt"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/viewedmovie"
)

const maxViewedMovies = 5000

// ViewedMovieIDs returns viewed codes and legacy JavDB IDs, newest first.
func (s *Service) ViewedMovieIDs(ctx context.Context) ([]string, error) {
	records, err := s.database.ViewedMovie.Query().
		Order(ent.Desc(viewedmovie.FieldViewedAt), ent.Desc(viewedmovie.FieldID)).
		Limit(maxViewedMovies).
		Select(viewedmovie.FieldCode, viewedmovie.FieldJavdbID).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load viewed movie IDs: %w", err)
	}
	ids := make([]string, 0, len(records))
	for _, r := range records {
		if r.Code != "" {
			ids = append(ids, r.Code)
		} else if r.JavdbID != "" {
			ids = append(ids, r.JavdbID)
		}
	}
	return ids, nil
}

// AddViewedMovieIDs records viewed codes or refreshes existing legacy IDs, evicting
// the oldest entries when the table exceeds maxViewedMovies.
func (s *Service) AddViewedMovieIDs(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now().UTC()
	seen := make(map[string]struct{}, len(ids))
	var clean []string
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		clean = append(clean, id)
	}
	if len(clean) == 0 {
		return nil
	}

	return ent.WithTx(ctx, s.database, func(tx *ent.Tx) error {
		latest, err := tx.ViewedMovie.Query().
			Order(ent.Desc(viewedmovie.FieldViewedAt)).
			First(ctx)
		if err != nil && !ent.IsNotFound(err) {
			return fmt.Errorf("load latest viewed movie: %w", err)
		}
		if latest != nil && !now.After(latest.ViewedAt) {
			now = latest.ViewedAt.Add(time.Microsecond)
		}

		legacyIDs, err := tx.ViewedMovie.Query().Where(viewedmovie.JavdbIDIn(clean...)).
			Select(viewedmovie.FieldJavdbID).Strings(ctx)
		if err != nil {
			return fmt.Errorf("load legacy viewed movie IDs: %w", err)
		}
		legacy := make(map[string]bool, len(legacyIDs))
		if len(legacyIDs) > 0 {
			if _, err := tx.ViewedMovie.Update().Where(viewedmovie.JavdbIDIn(legacyIDs...)).SetViewedAt(now).Save(ctx); err != nil {
				return fmt.Errorf("refresh legacy viewed movies: %w", err)
			}
			for _, id := range legacyIDs {
				legacy[id] = true
			}
		}
		var builders []*ent.ViewedMovieCreate
		for _, id := range clean {
			if !legacy[id] {
				builders = append(builders, tx.ViewedMovie.Create().SetCode(id).SetViewedAt(now))
			}
		}
		if len(builders) > 0 {
			err = tx.ViewedMovie.CreateBulk(builders...).
				OnConflictColumns(viewedmovie.FieldCode).
				UpdateViewedAt().
				Exec(ctx)
			if err != nil {
				return fmt.Errorf("upsert viewed movies: %w", err)
			}
		}

		_, err = tx.ViewedMovie.Delete().Where(func(s *sql.Selector) {
			viewed := sql.Table(viewedmovie.Table)
			// Keep the read order; SQLite's LIMIT -1 selects every row after
			// the retained prefix without an additional count query.
			oldest := sql.Select(viewed.C(viewedmovie.FieldID)).From(viewed).
				OrderBy(sql.Desc(viewed.C(viewedmovie.FieldViewedAt)), sql.Desc(viewed.C(viewedmovie.FieldID))).
				Limit(-1).Offset(maxViewedMovies)
			s.Where(sql.In(s.C(viewedmovie.FieldID), oldest))
		}).Exec(ctx)
		return err
	})
}
