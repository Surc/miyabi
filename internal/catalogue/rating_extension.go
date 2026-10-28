package catalogue

import (
	"context"
	"time"

	"github.com/ppxb/miyabi/internal/codeid"
	"github.com/ppxb/miyabi/internal/domain"
)

// ExternalRatingReader is optional and independent of the comment client.
// Ownership transfers to MovieExtras; pass nil to preserve its default reader.
type ExternalRatingReader interface {
	Rating(context.Context, string) (domain.ExternalRating, error)
	Close()
}

type ratingExtension struct {
	reader ExternalRatingReader
	cache  *responseCache[domain.ExternalRating]
}

// NewMovieExtrasWithRating keeps the existing constructor and public API intact.
// Ratings have a separate cache so a failed comment lookup cannot replace them.
func NewMovieExtrasWithRating(service *Service, reader ExternalRatingReader) *MovieExtras {
	extras := NewMovieExtras(service)
	if reader != nil {
		extras.rating = &ratingExtension{reader: reader, cache: newResponseCache[domain.ExternalRating](256, 10*time.Minute)}
	}
	return extras
}

func (r *ratingExtension) rating(ctx context.Context, code string) (domain.ExternalRating, error) {
	code = codeid.Normalize(code)
	return r.cache.get(ctx, code, func(ctx context.Context) (domain.ExternalRating, error) {
		return r.reader.Rating(ctx, code)
	})
}
