package subtitle

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/ppxb/miyabi/internal/codeid"
	subtitlemeta "github.com/ppxb/miyabi/internal/domain/subtitle"
	"github.com/ppxb/miyabi/internal/netx"
)

// Provider searches one online subtitle source.
type Provider interface {
	Search(ctx context.Context, query string) ([]Candidate, error)
}

// Finder searches online providers and downloads their subtitles.
type Finder struct {
	providers []Provider
	client    *http.Client
}

type FinderOption func(*Finder)

// WithProviders replaces the default providers.
func WithProviders(providers ...Provider) FinderOption {
	return func(f *Finder) { f.providers = providers }
}

func NewFinder(proxyManager *netx.ProxyManager, opts ...FinderOption) *Finder {
	f := &Finder{
		providers: []Provider{
			NewXunleiProvider(proxyManager),
			NewSubtitleCatProvider(proxyManager),
			NewAVSubtitlesProvider(proxyManager),
		},
		client: netx.NewSafeDownloadClient(proxyManager, 20*time.Second),
	}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

// Search returns the ranked candidates for a catalogue number. Release names
// may decorate the number catalogue sites use (259LUXU-1899 for LUXU-1899),
// so relaxed forms are queried only when the exact number finds nothing.
func (f *Finder) Search(ctx context.Context, code string, uncensored bool) []Candidate {
	candidates, _ := f.SearchWithConfig(ctx, code, uncensored, DefaultConfig())
	return candidates
}

func (f *Finder) SearchWithConfig(ctx context.Context, code string, uncensored bool, config Config) ([]Candidate, error) {
	if !config.Enabled || !config.Sources.Any() {
		return nil, nil
	}
	var errs []error
	for _, query := range codeid.Candidates(code) {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(append(errs, err)...)
		}
		candidates, err := f.search(ctx, query, config.Sources)
		errs = append(errs, err)
		if ranked := Rank(candidates, code, uncensored, config.Language); len(ranked) > 0 {
			return ranked, errors.Join(errs...)
		}
	}
	return nil, errors.Join(errs...)
}

// search queries enabled providers concurrently and reports errors alongside
// usable results, so a failing provider never hides the others' candidates.
func (f *Finder) search(ctx context.Context, query string, sources Sources) ([]Candidate, error) {
	var (
		mu         sync.Mutex
		wg         sync.WaitGroup
		candidates []Candidate
		errs       []error
		seen       = make(map[string]bool)
	)
	for _, provider := range f.providers {
		id, name := providerSource(provider)
		if !sources.enabled(id) {
			continue
		}
		wg.Go(func() {
			results, err := provider.Search(ctx, query)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", name, err))
			}
			for _, candidate := range results {
				if candidate.URL != "" && !seen[candidate.URL] {
					seen[candidate.URL] = true
					candidates = append(candidates, candidate)
				}
			}
		})
	}
	wg.Wait()
	return candidates, errors.Join(errs...)
}

func providerSource(provider Provider) (string, string) {
	switch provider.(type) {
	case *XunleiProvider:
		return "xunlei", "迅雷"
	case *SubtitleCatProvider:
		return "subtitlecat", "SubtitleCat"
	case *AVSubtitlesProvider:
		return "avsubtitles", "AVSubtitles"
	default:
		return "", "字幕源"
	}
}

// Download fetches a candidate and resolves an unknown language from its text.
func (f *Finder) Download(ctx context.Context, candidate Candidate) ([]byte, subtitlemeta.Language, error) {
	if candidate.URL == "" {
		return nil, "", fmt.Errorf("empty candidate URL")
	}
	var raw []byte
	var err error
	if candidate.download != nil {
		raw, err = candidate.download.fetch(ctx, f.client, candidate)
	} else {
		raw, err = netx.SafeDownload(ctx, f.client, candidate.URL)
	}
	if err != nil {
		return nil, "", fmt.Errorf("download subtitle: %w", err)
	}
	body, text, err := Normalize(raw, candidate.Format)
	if err != nil {
		return nil, "", err
	}
	language := candidate.Language
	if language == subtitlemeta.LangUnknown {
		language = subtitlemeta.DetectLanguage("", text)
	}
	return body, language, nil
}
