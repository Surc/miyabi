package catalogue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/magnet"
)

type BrowseCacheSettings struct {
	Pages         int `json:"pages"`
	RatingHours   int `json:"rating_hours"`
	MagnetMinutes int `json:"magnet_minutes"`
	ListMinutes   int `json:"list_minutes"`
}

func DefaultBrowseCacheSettings() BrowseCacheSettings { return BrowseCacheSettings{6, 12, 30, 5} }
func (c BrowseCacheSettings) Valid() bool {
	return c.Pages >= 1 && c.Pages <= 24 && c.RatingHours >= 1 && c.RatingHours <= 168 && c.MagnetMinutes >= 1 && c.MagnetMinutes <= 1440 && c.ListMinutes >= 1 && c.ListMinutes <= 60
}

type BrowseCached[T any] struct {
	Data          T         `json:"data"`
	UpdatedAt     time.Time `json:"updated_at"`
	Stale         bool      `json:"stale"`
	RefreshFailed bool      `json:"refresh_failed,omitempty"`
}
type BrowseResources struct {
	Movie       domain.MovieDetail `json:"movie"`
	Magnets     []Magnet           `json:"magnets"`
	Complete    bool               `json:"complete"`
	HasSubtitle bool               `json:"has_subtitle"`
	HasCracked  bool               `json:"has_cracked"`
}
type browseReference struct {
	Code  string `json:"code"`
	Cover string `json:"cover"`
}
type browseExtensionSource interface {
	Browse(context.Context, domain.BrowseOptions) ([]Movie, error)
	Search(context.Context, string, domain.SearchOptions) ([]Movie, error)
	CatalogueDetail(context.Context, string) (domain.MovieDetail, error)
	BrowseResourceSnapshot(context.Context, domain.MovieDetail) ([]Magnet, bool, error)
	Media(context.Context, string) (domain.Media, error)
}
type browseRatingReader interface {
	JavLibraryRating(context.Context, string) (domain.ExternalRating, error)
}
type browseGenerationKey struct{}

func browseGeneration(ctx context.Context, current uint64) uint64 {
	if inherited, ok := ctx.Value(browseGenerationKey{}).(uint64); ok {
		return inherited
	}
	return current
}

// BrowseExtension is opt-in: durable data and fresh resource summaries never
// change the upstream catalogue interfaces, ratings, ranking or download picker.
type BrowseExtension struct {
	source     browseExtensionSource
	ratings    browseRatingReader
	store      *browseStore
	loads      *responseCache[json.RawMessage]
	settingsMu sync.RWMutex
	settings   BrowseCacheSettings
	now        func() time.Time
}

func NewBrowseExtension(directory string, source browseExtensionSource, ratings browseRatingReader) (*BrowseExtension, error) {
	store, err := newBrowseStore(directory)
	if err != nil {
		return nil, err
	}
	s := &BrowseExtension{source: source, ratings: ratings, store: store, loads: newResponseCache[json.RawMessage](256, time.Second), settings: DefaultBrowseCacheSettings(), now: time.Now}
	if r, ok, _ := store.read("config", "settings"); ok {
		var settings BrowseCacheSettings
		if json.Unmarshal(r.Data, &settings) == nil && settings.Valid() {
			s.settings = settings
		}
	}
	return s, nil
}
func (s *BrowseExtension) Settings() BrowseCacheSettings {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.settings
}
func (s *BrowseExtension) UpdateSettings(settings BrowseCacheSettings) (BrowseCacheSettings, error) {
	if !settings.Valid() {
		return s.Settings(), domain.E(domain.KindInvalid, "预加载或缓存有效期超出允许范围", nil)
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	data, _ := json.Marshal(settings)
	_, _, generation := s.store.read("config", "settings")
	if err := s.store.write("config", "settings", browseRecord{s.now(), data}, generation); err != nil {
		return s.settings, err
	}
	s.settings = settings
	return settings, nil
}
func (s *BrowseExtension) CacheStats() (BrowseCacheStats, error) { return s.store.stats() }
func (s *BrowseExtension) ClearCache(scope string) (BrowseCacheStats, error) {
	if !slices.Contains([]string{"all", "images", "data"}, scope) {
		return BrowseCacheStats{}, domain.E(domain.KindInvalid, "不支持的缓存清理范围", nil)
	}
	stats, err := s.store.clear(scope)
	s.loads.reset()
	return stats, err
}

// Initial reads return last-known data immediately. A separate refresh request
// revalidates only stale values; failures retain the last successful snapshot.
func browseLoad[T any](ctx context.Context, s *BrowseExtension, namespace, key string, ttl time.Duration, refresh bool, load func(context.Context) (T, bool, error)) (BrowseCached[T], error) {
	if err := ctx.Err(); err != nil {
		return BrowseCached[T]{}, err
	}
	r, ok, generation := s.store.read(namespace, key)
	generation = browseGeneration(ctx, generation)
	var cached BrowseCached[T]
	if ok {
		ok = json.Unmarshal(r.Data, &cached.Data) == nil
	}
	if ok {
		cached.UpdatedAt, cached.Stale = r.UpdatedAt, ttl > 0 && s.now().Sub(r.UpdatedAt) >= ttl
		if !refresh || !cached.Stale {
			return cached, nil
		}
	}
	var result BrowseCached[T]
	callKey := fmt.Sprintf("%d:%s:%s", generation, namespace, key)
	data, err := s.loads.get(ctx, callKey, func(ctx context.Context) (json.RawMessage, error) {
		ctx = context.WithValue(ctx, browseGenerationKey{}, generation)
		value, complete, err := load(ctx)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result := BrowseCached[T]{Data: value, UpdatedAt: s.now(), Stale: !complete}
		body, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		if complete {
			if err := s.store.write(namespace, key, browseRecord{result.UpdatedAt, body}, generation); err != nil {
				return nil, err
			}
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		if !complete {
			return encoded, ErrDoNotCache
		}
		return encoded, nil
	})
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if ok {
			cached.Stale, cached.RefreshFailed = true, true
			slog.WarnContext(ctx, "discovery cache refresh failed", "namespace", namespace, "error", err)
			return cached, nil
		}
		return result, domain.E(domain.KindUpstream, "作品资料或资源暂不可用，请重试", err)
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return result, err
	}
	return result, nil
}
func (s *BrowseExtension) BrowsePage(ctx context.Context, options domain.BrowseOptions, refresh bool) (BrowseCached[[]Movie], error) {
	key := fmt.Sprintf("browse:%#v", options)
	_, _, generation := s.store.read("indexes", key)
	generation = browseGeneration(ctx, generation)
	return browseLoad(ctx, s, "indexes", key, time.Duration(s.Settings().ListMinutes)*time.Minute, refresh, func(ctx context.Context) ([]Movie, bool, error) {
		items, err := s.source.Browse(ctx, options)
		if err == nil && ctx.Err() == nil {
			s.seedRatings(items, generation)
		}
		return items, true, err
	})
}
func (s *BrowseExtension) SearchPage(ctx context.Context, query string, options domain.SearchOptions, refresh bool) (BrowseCached[[]Movie], error) {
	key := fmt.Sprintf("search:%q:%#v", query, options)
	_, _, generation := s.store.read("indexes", key)
	generation = browseGeneration(ctx, generation)
	return browseLoad(ctx, s, "indexes", key, time.Duration(s.Settings().ListMinutes)*time.Minute, refresh, func(ctx context.Context) ([]Movie, bool, error) {
		items, err := s.source.Search(ctx, query, options)
		if err == nil && ctx.Err() == nil {
			s.seedRatings(items, generation)
		}
		return items, true, err
	})
}
func (s *BrowseExtension) seedRatings(items []Movie, generation uint64) {
	for _, item := range items {
		reference := browseReference{Code: item.Code, Cover: item.Cover}
		if old, ok, _ := s.store.read("references", item.ID); ok {
			var previous browseReference
			if json.Unmarshal(old.Data, &previous) == nil {
				if reference.Code == "" {
					reference.Code = previous.Code
				}
				if reference.Cover == "" {
					reference.Cover = previous.Cover
				}
			}
		}
		ref, _ := json.Marshal(reference)
		if err := s.store.write("references", item.ID, browseRecord{s.now(), ref}, generation); err != nil {
			slog.Warn("persist browse reference", "error", err)
		}
		value, max := item.Rating, int(item.RatingMax)
		if max == 0 {
			max = 5
		}
		rating := domain.ExternalRating{Source: item.RatingSource, Score: &value, Max: max, Found: true}
		if value <= 0 {
			rating.Score = nil
		}
		if rating.Source == "" {
			rating.Source = "javdb"
		}
		data, _ := json.Marshal(rating)
		if err := s.store.write("ratings", "javdb:"+item.ID, browseRecord{s.now(), data}, generation); err != nil {
			slog.Warn("persist browse rating", "error", err)
		}
	}
}
func (s *BrowseExtension) movie(ctx context.Context, id string) (domain.MovieDetail, error) {
	_, _, generation := s.store.read("works", id)
	generation = browseGeneration(ctx, generation)
	result, err := browseLoad(ctx, s, "works", id, 0, false, func(ctx context.Context) (domain.MovieDetail, bool, error) {
		item, err := s.source.CatalogueDetail(ctx, id)
		// Dynamic values are stored independently and never become permanent metadata.
		if err == nil && ctx.Err() == nil {
			if item.ID != id {
				return domain.MovieDetail{}, false, fmt.Errorf("mismatched movie identity")
			}
			s.seedRatings([]Movie{{Movie: item.Movie}}, generation)
		}
		item.Rating, item.MagnetsCount, item.HasSubtitle = 0, 0, false
		return item, true, err
	})
	return result.Data, err
}
func (s *BrowseExtension) Resources(ctx context.Context, id string, refresh bool) (BrowseCached[BrowseResources], error) {
	return browseLoad(ctx, s, "resources", id, time.Duration(s.Settings().MagnetMinutes)*time.Minute, refresh, func(ctx context.Context) (BrowseResources, bool, error) {
		movie, err := s.movie(ctx, id)
		if err != nil {
			return BrowseResources{}, false, err
		}
		magnets, complete, err := s.source.BrowseResourceSnapshot(ctx, movie)
		result := BrowseResources{Movie: movie, Magnets: magnets, Complete: complete}
		for _, item := range magnets {
			result.HasSubtitle = result.HasSubtitle || magnet.HasSubtitle(item.Magnet)
			result.HasCracked = result.HasCracked || slices.Contains(item.Tags, domain.MagnetTagCracked)
		}
		return result, complete, err
	})
}
func (s *BrowseExtension) Rating(ctx context.Context, source, id string, refresh bool) (BrowseCached[domain.ExternalRating], error) {
	return browseLoad(ctx, s, "ratings", source+":"+id, time.Duration(s.Settings().RatingHours)*time.Hour, refresh, func(ctx context.Context) (domain.ExternalRating, bool, error) {
		if source == "javlibrary" {
			ref, err := s.reference(ctx, id)
			if err != nil {
				return domain.ExternalRating{}, false, err
			}
			if s.ratings == nil {
				return domain.ExternalRating{}, false, fmt.Errorf("rating source unavailable")
			}
			rating, err := s.ratings.JavLibraryRating(ctx, ref.Code)
			return rating, true, err
		}
		movie, err := s.source.CatalogueDetail(ctx, id)
		max := int(movie.RatingMax)
		if max == 0 {
			max = 5
		}
		provider := movie.RatingSource
		if provider == "" {
			provider = "javdb"
		}
		rating := domain.ExternalRating{Source: provider, Score: &movie.Rating, Max: max, Found: true}
		if movie.Rating <= 0 {
			rating.Score = nil
		}
		return rating, true, err
	})
}
func (s *BrowseExtension) Image(ctx context.Context, id string) (domain.Media, error) {
	_, _, generation := s.store.read("works", id)
	ctx = context.WithValue(ctx, browseGenerationKey{}, browseGeneration(ctx, generation))
	ref, err := s.reference(ctx, id)
	if err != nil {
		return domain.Media{}, err
	}
	// Target URLs come only from verified metadata, never from request parameters.
	result, err := browseLoad(ctx, s, "images", id+":"+ref.Cover, 0, false, func(ctx context.Context) (domain.Media, bool, error) {
		media, err := s.source.Media(ctx, ref.Cover)
		return media, true, err
	})
	return result.Data, err
}

// Lists already supply trustworthy cover URLs and codes. Images and optional
// ratings must not fan out full-detail requests ahead of the resource queue.
func (s *BrowseExtension) reference(ctx context.Context, id string) (browseReference, error) {
	r, ok, _ := s.store.read("references", id)
	var ref browseReference
	if ok && json.Unmarshal(r.Data, &ref) == nil && ref.Code != "" {
		return ref, nil
	}
	movie, err := s.movie(ctx, id)
	return browseReference{Code: movie.Code, Cover: movie.Cover}, err
}
