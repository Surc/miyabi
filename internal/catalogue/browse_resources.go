package catalogue

import (
	"context"
	"github.com/ppxb/miyabi/internal/domain"
)

// BrowseResourceSnapshot exposes completeness to the independent browse cache.
// Existing Magnets/CatalogueMagnets responses and their cache semantics stay intact.
func (s *Service) BrowseResourceSnapshot(ctx context.Context, movie domain.MovieDetail) ([]Magnet, bool, error) {
	items, partial, err := s.aggregator.FindDetailed(ctx, domain.MovieRef{JavDBID: movie.ID, Code: movie.Code, Zone: movie.Zone})
	if err == nil {
		_ = s.persistActiveRoute(ctx)
	}
	return projectMagnets(items), !partial, err
}
