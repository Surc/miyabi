package catalogue

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/ppxb/miyabi/internal/domain"
)

func (s *BrowseExtension) Image(ctx context.Context, id string) (domain.Media, error) {
	_, _, generation := s.store.read("works", id)
	ctx = context.WithValue(ctx, browseGenerationKey{}, browseGeneration(ctx, generation))
	ref, err := s.reference(ctx, id)
	if err != nil {
		return domain.Media{}, err
	}
	return s.media(ctx, ref.Cover, id+":"+ref.Cover)
}

// Media retains the images fetched by the discovery proxy.
func (s *BrowseExtension) Media(ctx context.Context, url string) (domain.Media, error) {
	return s.media(ctx, url, "")
}

func (s *BrowseExtension) media(ctx context.Context, url, legacyKey string) (domain.Media, error) {
	media, err := s.cachedMedia(ctx, "url:"+url, func(ctx context.Context) (domain.Media, error) {
		// Covers cached before the proxy and list shared a key can be reused on demand.
		if legacyKey != "" {
			if record, ok, _ := s.store.read("images", legacyKey); ok {
				var media domain.Media
				if json.Unmarshal(record.Data, &media) == nil && isImage(media) {
					return media, nil
				}
			}
		}
		return s.source.Media(ctx, url)
	})
	if err != nil && ctx.Err() == nil {
		return media, domain.E(domain.KindUpstream, "作品资料或资源暂不可用，请重试", err)
	}
	return media, err
}

// PreviewImage caches the saved library candidate without changing its provider adapter.
func (s *BrowseExtension) PreviewImage(ctx context.Context, candidate domain.ImageCandidate, fetch func(context.Context, domain.ImageCandidate) (domain.Media, error)) (domain.Media, error) {
	key := "preview:" + candidate.Provider + ":" + candidate.URL
	if candidate.Provider == "javdb" {
		key = "url:" + candidate.URL
	}
	return s.cachedMedia(ctx, key, func(ctx context.Context) (domain.Media, error) {
		return fetch(ctx, candidate)
	})
}

func (s *BrowseExtension) cachedMedia(ctx context.Context, key string, fetch func(context.Context) (domain.Media, error)) (domain.Media, error) {
	result, err := loadBrowseRecord(ctx, s, "images", key, 0, false, func(ctx context.Context) (domain.Media, bool, error) {
		media, err := fetch(ctx)
		return media, isImage(media), err
	})
	return result.Data, err
}

func isImage(media domain.Media) bool {
	return len(media.Body) > 0 && strings.HasPrefix(media.ContentType, "image/")
}
