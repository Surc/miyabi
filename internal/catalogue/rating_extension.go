package catalogue

import (
	"context"
	"time"

	"github.com/ppxb/miyabi/internal/codeid"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/javlibrary"
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

func NewMovieExtrasWithRating(service *Service, reader ExternalRatingReader) *MovieExtras {
	extras := NewMovieExtras(service)
	switch reader.(type) {
	case *javlibrary.Client, *javlibrary.BrowserRelay:
		// These readers derive their score from Lookup and can share it with comments.
		extras.library.Close()
		extras.library = reader.(librarySocialReader)
	case nil:
	default:
		extras.rating = &ratingExtension{reader: reader, cache: newResponseCache[domain.ExternalRating](256, 10*time.Minute)}
	}
	return extras
}

func (r *ratingExtension) rating(ctx context.Context, code string) (domain.ExternalRating, error) {
	code = codeid.Normalize(code)
	return r.cache.get(ctx, libraryCacheKey(ctx, code), func(ctx context.Context) (domain.ExternalRating, error) {
		return r.reader.Rating(ctx, code)
	})
}

func libraryCacheKey(ctx context.Context, key string) string {
	if peer := javlibrary.BrowserPeer(ctx); peer != "" {
		return key + "@" + peer
	}
	return key
}
