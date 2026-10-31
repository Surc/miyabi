package catalogue

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ppxb/miyabi/internal/codeid"
	"github.com/ppxb/miyabi/internal/domain"
)

// CachedRating never contacts upstream, so cached scores bypass the refresh queue.
func (s *BrowseExtension) CachedRating(source, id string) *BrowseCached[domain.ExternalRating] {
	r, ok, _ := s.store.read("ratings", browseRatingKey(source, id))
	if source == "javlibrary" {
		if reference, found, _ := s.store.read("references", id); found {
			var ref browseReference
			if json.Unmarshal(reference.Data, &ref) == nil {
				if rating, found, _ := s.store.read("ratings", browseRatingKey("javlibrary-code", codeid.Normalize(ref.Code))); found && (!ok || rating.UpdatedAt.After(r.UpdatedAt)) {
					r, ok = rating, true
				}
			}
		}
	}
	var result BrowseCached[domain.ExternalRating]
	if !ok || json.Unmarshal(r.Data, &result.Data) != nil {
		return nil
	}
	result.UpdatedAt = r.UpdatedAt
	result.Stale = s.now().Sub(r.UpdatedAt) >= time.Duration(s.Settings().RatingHours)*time.Hour
	return &result
}

func (s *BrowseExtension) Tags(ctx context.Context, zone domain.Zone, refresh bool) (BrowseCached[[]domain.TagCategory], error) {
	return browseLoad(ctx, s, "taxonomy", string(zone), 24*time.Hour, refresh, func(ctx context.Context) ([]domain.TagCategory, bool, error) {
		reader, ok := s.source.(interface {
			Tags(context.Context, domain.Zone) ([]domain.TagCategory, error)
		})
		if !ok {
			return nil, false, fmt.Errorf("tag source unavailable")
		}
		value, err := reader.Tags(ctx, zone)
		return value, true, err
	})
}

func (s *BrowseExtension) RatingByCode(ctx context.Context, code string, refresh bool) (BrowseCached[domain.ExternalRating], error) {
	code = codeid.Normalize(code)
	return browseLoad(ctx, s, "ratings", browseRatingKey("javlibrary-code", code), time.Duration(s.Settings().RatingHours)*time.Hour, refresh, func(ctx context.Context) (domain.ExternalRating, bool, error) {
		if s.ratings == nil {
			return domain.ExternalRating{}, false, fmt.Errorf("rating source unavailable")
		}
		rating, err := s.ratings.JavLibraryRating(ctx, code)
		return rating, true, err
	})
}

// Media also retains viewed previews and actor pictures fetched by the image proxy.
func (s *BrowseExtension) Media(ctx context.Context, url string) (domain.Media, error) {
	result, err := browseLoad(ctx, s, "images", "url:"+url, 0, false, func(ctx context.Context) (domain.Media, bool, error) {
		media, err := s.source.Media(ctx, url)
		return media, strings.HasPrefix(media.ContentType, "image/"), err
	})
	return result.Data, err
}
