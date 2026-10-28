package catalogue

import (
	"context"
	"fmt"
	"time"

	"github.com/ppxb/miyabi/internal/codeid"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/javlibrary"
)

type commentReader interface {
	MovieComments(context.Context, string, int) (domain.MovieComments, error)
}
type librarySocialReader interface {
	Lookup(context.Context, string) (javlibrary.Film, error)
	Comments(context.Context, string, int) (domain.MovieComments, error)
	Close()
}

// MovieExtras is an opt-in extension. Its caches never modify saved metadata,
// the existing catalogue responses, or automatic magnet selection.
type MovieExtras struct {
	catalogue *Service
	library   librarySocialReader
	films     *responseCache[javlibrary.Film]
	comments  *responseCache[domain.MovieComments]
	rating    *ratingExtension
}

func NewMovieExtras(service *Service) *MovieExtras {
	return &MovieExtras{catalogue: service, library: javlibrary.New(service.proxy), films: newResponseCache[javlibrary.Film](256, 10*time.Minute), comments: newResponseCache[domain.MovieComments](512, 2*time.Minute)}
}
func (e *MovieExtras) Close() {
	e.library.Close()
	if e.rating != nil {
		e.rating.reader.Close()
	}
}
func (e *MovieExtras) film(ctx context.Context, code string) (javlibrary.Film, error) {
	code = codeid.Normalize(code)
	return e.films.get(ctx, code, func(ctx context.Context) (javlibrary.Film, error) { return e.library.Lookup(ctx, code) })
}
func (e *MovieExtras) JavLibraryRating(ctx context.Context, code string) (domain.ExternalRating, error) {
	if e.rating != nil {
		rating, err := e.rating.rating(ctx, code)
		return rating, extrasError(ctx, err)
	}
	film, err := e.film(ctx, code)
	return film.Rating, extrasError(ctx, err)
}
func (e *MovieExtras) MovieComments(ctx context.Context, source, id, code string, page int) (domain.MovieComments, error) {
	empty := domain.MovieComments{Source: source, Page: page, Items: []domain.MovieComment{}}
	if page < 1 || page > 1000 {
		return empty, domain.E(domain.KindInvalid, "无效的评论页码", nil)
	}
	var result domain.MovieComments
	var err error
	switch source {
	case "javdb":
		reader, ok := e.catalogue.javdb.(commentReader)
		if !ok {
			return empty, domain.E(domain.KindUpstream, "当前 JavDB 客户端不支持评论", nil)
		}
		key := fmt.Sprintf("javdb:%s:%d", id, page)
		result, err = cachedJavDB(ctx, e.catalogue, e.comments, key, func(ctx context.Context) (domain.MovieComments, error) { return reader.MovieComments(ctx, id, page) })
	case "javlibrary":
		film, lookupErr := e.film(ctx, code)
		if lookupErr != nil {
			return empty, extrasError(ctx, lookupErr)
		}
		if film.ID == "" {
			return empty, nil
		}
		key := fmt.Sprintf("javlibrary:%s:%d", film.ID, page)
		result, err = e.comments.get(ctx, key, func(ctx context.Context) (domain.MovieComments, error) { return e.library.Comments(ctx, film.ID, page) })
	default:
		return empty, domain.E(domain.KindInvalid, "不支持的评论来源", nil)
	}
	return result, extrasError(ctx, err)
}
func extrasError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if domain.IsKind(err, domain.KindInvalid) {
		return err
	}
	return domain.E(domain.KindUpstream, "外部评分或评论暂不可用，请稍后重试", err)
}
