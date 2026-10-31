package subtitle

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/ppxb/miyabi/internal/domain"
	subtitlemeta "github.com/ppxb/miyabi/internal/domain/subtitle"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/netx"
)

func TestNullSubtitleConfigCannotResetDisabledSettings(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = false
	before := config
	if err := json.Unmarshal([]byte("null"), &config); err == nil || config != before {
		t.Fatalf("null config reset disabled settings: config=%+v error=%v", config, err)
	}
}

type canceledSearchProvider struct{ calls atomic.Int32 }

func (p *canceledSearchProvider) Search(ctx context.Context, _ string) ([]Candidate, error) {
	p.calls.Add(1)
	return nil, ctx.Err()
}

func TestCanceledSearchDoesNotStartProvidersOrFallbackQueries(t *testing.T) {
	provider := &canceledSearchProvider{}
	finder := NewFinder(nil, WithProviders(provider))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := finder.SearchWithConfig(ctx, "259LUXU-1899", false, DefaultConfig())
	if err == nil || provider.calls.Load() != 0 {
		t.Fatalf("canceled search: calls=%d error=%v", provider.calls.Load(), err)
	}
}

func TestSettingsDefaultAndPersistWithoutReenablingSources(t *testing.T) {
	service, db, _, _ := exportFixture(t, nil)
	config, err := service.Config(t.Context())
	if err != nil || config != DefaultConfig() {
		t.Fatalf("defaults = %+v, %v", config, err)
	}
	var partial Config
	if err := json.Unmarshal([]byte(`{"enabled":false,"sources":{"xunlei":false}}`), &partial); err != nil {
		t.Fatal(err)
	}
	if partial.Enabled || partial.Sources.Xunlei || !partial.Sources.SubtitleCat || !partial.Sources.AVSubtitles || partial.Language != subtitlemeta.LangSimplifiedChinese {
		t.Fatalf("partial settings = %+v", partial)
	}
	want := Config{Enabled: false, Sources: Sources{}, Language: subtitlemeta.LangTraditionalChinese}
	if saved, err := service.UpdateConfig(t.Context(), want); err != nil || saved != want {
		t.Fatalf("save = %+v, %v", saved, err)
	}
	restarted := NewService(db, NewFinder(nil))
	if got, err := restarted.Config(t.Context()); err != nil || got != want {
		t.Fatalf("restarted settings = %+v, %v", got, err)
	}
	invalid := want
	invalid.Language = "en"
	if _, err := service.UpdateConfig(t.Context(), invalid); !domain.IsKind(err, domain.KindInvalid) {
		t.Fatalf("invalid language error = %v", err)
	}
	if got, err := service.Config(t.Context()); err != nil || got != want {
		t.Fatalf("invalid update changed settings: %+v, %v", got, err)
	}
}

func TestDisabledAutoDownloadPreservesExportedSubtitles(t *testing.T) {
	service, db, movieID, target := exportFixture(t, map[string]string{"/file.srt": simplifiedSRT},
		Candidate{Provider: "迅雷", Name: "ABP-123.chs.srt", URL: "/file.srt", Format: "srt", Language: subtitlemeta.LangSimplifiedChinese, Version: subtitlemeta.VersionStandard})
	if n, err := service.Export(t.Context(), movieID, target); err != nil || n != 1 {
		t.Fatalf("initial export = %d, %v", n, err)
	}
	track := db.Subtitle.Query().OnlyX(t.Context())
	before, err := os.ReadFile(track.StoragePath)
	if err != nil {
		t.Fatal(err)
	}
	service.finder.client.Transport = subtitleTransport(func(*http.Request) (*http.Response, error) {
		t.Error("disabled auto-download made a network request")
		return nil, nil
	})
	for _, sourceDisabled := range []bool{false, true} {
		config := DefaultConfig()
		if sourceDisabled {
			config.Sources = Sources{}
		} else {
			config.Enabled = false
		}
		if _, err := service.UpdateConfig(t.Context(), config); err != nil {
			t.Fatal(err)
		}
		target.Dir = filepath.Join(t.TempDir(), "new-export")
		if n, err := service.Export(t.Context(), movieID, target); err != nil || n != 0 {
			t.Fatalf("disabled export = %d, %v", n, err)
		}
		if got := db.Subtitle.Query().OnlyX(t.Context()); got.StoragePath != track.StoragePath {
			t.Fatal("disabled export changed the saved subtitle")
		}
		if after, err := os.ReadFile(track.StoragePath); err != nil || string(after) != string(before) {
			t.Fatalf("existing subtitle changed: %v", err)
		}
		if _, err := os.Stat(target.Dir); !os.IsNotExist(err) {
			t.Fatalf("disabled export created destination: %v", err)
		}
	}
}

func TestConfiguredSourcesSkipRequestsAndPreserveOtherSources(t *testing.T) {
	var xunlei, cat, avsub atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/xunlei":
			xunlei.Add(1)
			io.WriteString(w, `{"code":0,"data":[{"name":"ABP-123.srt","url":"https://example.com/xunlei.srt","ext":"srt","languages":["zh-CN"]}]}`)
		case "/index.php":
			cat.Add(1)
			io.WriteString(w, `<a href="/subs/123/movie.html">ABP-123</a>`)
		case "/subs/123/movie.html":
			cat.Add(1)
			io.WriteString(w, `<div class="sub-single"><img class="flag" alt="zh-TW"><a class="green-link" href="https://example.com/cat.srt">Download</a></div>`)
		case "/search_results.php":
			avsub.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			t.Errorf("unexpected request: %s", r.URL)
		}
	}))
	defer server.Close()
	proxy, err := netx.NewProxyManager(netx.ProxyConfig{Enabled: true, URL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	finder := NewFinder(proxy)
	for _, source := range finder.providers {
		switch source := source.(type) {
		case *XunleiProvider:
			source.endpoint = "http://93.184.216.34/xunlei"
		case *SubtitleCatProvider:
			source.origin = "http://93.184.216.34"
		case *AVSubtitlesProvider:
			source.origin = "http://93.184.216.34"
		}
	}
	for _, test := range []struct {
		sources Sources
		want    string
	}{
		{Sources{Xunlei: true}, "迅雷"},
		{Sources{SubtitleCat: true}, "SubtitleCat"},
		{Sources{}, ""},
	} {
		xunlei.Store(0)
		cat.Store(0)
		config := DefaultConfig()
		config.Sources = test.sources
		candidates, err := finder.SearchWithConfig(t.Context(), "ABP-123", false, config)
		if err != nil || avsub.Load() != 0 || xunlei.Load() > 0 != test.sources.Xunlei || cat.Load() > 0 != test.sources.SubtitleCat {
			t.Fatalf("source requests xunlei=%d cat=%d avsub=%d error=%v", xunlei.Load(), cat.Load(), avsub.Load(), err)
		}
		if test.want == "" {
			if len(candidates) != 0 {
				t.Fatal("all disabled sources returned candidates")
			}
		} else if len(candidates) != 1 || candidates[0].Provider != test.want {
			t.Fatalf("selected candidates = %+v", candidates)
		}
	}
}

func TestLanguagePreferenceKeepsOtherChineseTracksAvailable(t *testing.T) {
	candidates := []Candidate{
		{Name: "ABP-123.chs.srt", Format: "srt", Language: subtitlemeta.LangSimplifiedChinese, Version: subtitlemeta.VersionStandard},
		{Name: "ABP-123.cht.srt", Format: "srt", Language: subtitlemeta.LangTraditionalChinese, Version: subtitlemeta.VersionStandard},
	}
	for _, preferred := range []subtitlemeta.Language{subtitlemeta.LangSimplifiedChinese, subtitlemeta.LangTraditionalChinese} {
		ranked := Rank(candidates, "ABP-123", false, preferred)
		if len(ranked) != 2 || ranked[0].Language != preferred {
			t.Fatalf("preference %s: %+v", preferred, ranked)
		}
	}
	service, db, movieID, target := exportFixture(t, map[string]string{"/chs.srt": simplifiedSRT, "/cht.srt": traditionalSRT},
		Candidate{Name: "ABP-123.chs.srt", URL: "/chs.srt", Format: "srt", Language: subtitlemeta.LangSimplifiedChinese, Version: subtitlemeta.VersionStandard},
		Candidate{Name: "ABP-123.cht.srt", URL: "/cht.srt", Format: "srt", Language: subtitlemeta.LangTraditionalChinese, Version: subtitlemeta.VersionStandard})
	config := DefaultConfig()
	config.Language = subtitlemeta.LangTraditionalChinese
	if _, err := service.UpdateConfig(t.Context(), config); err != nil {
		t.Fatal(err)
	}
	if n, err := service.Export(t.Context(), movieID, target); n != 2 || err != nil {
		t.Fatalf("preferred export = %d, %v", n, err)
	}
	if tracks := db.Subtitle.Query().Order(ent.Asc("id")).AllX(t.Context()); tracks[0].Language != string(config.Language) {
		t.Fatalf("first exported language = %s", tracks[0].Language)
	}
}
