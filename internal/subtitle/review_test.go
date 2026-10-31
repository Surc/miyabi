package subtitle

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ppxb/miyabi/internal/netx"
)

func avsubReviewProvider(t *testing.T, handler http.HandlerFunc) *AVSubtitlesProvider {
	t.Helper()
	proxy := httptest.NewServer(handler)
	t.Cleanup(proxy.Close)
	manager, err := netx.NewProxyManager(netx.ProxyConfig{Enabled: true, URL: proxy.URL})
	if err != nil {
		t.Fatal(err)
	}
	provider := NewAVSubtitlesProvider(manager)
	provider.origin = "http://93.184.216.34"
	return provider
}

func TestAVSubtitlesSearchBlocksPrivateRedirects(t *testing.T) {
	var privateRequests atomic.Int32
	provider := avsubReviewProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/private" {
			privateRequests.Add(1)
			io.WriteString(w, "private content")
			return
		}
		http.Redirect(w, r, "http://127.0.0.1/private", http.StatusFound)
	})
	_, err := provider.Search(t.Context(), "ABP-123")
	if err == nil || !strings.Contains(err.Error(), "redirect blocked") || privateRequests.Load() != 0 {
		t.Fatalf("private redirect: requests=%d error=%v", privateRequests.Load(), err)
	}
}

func TestAVSubtitlesDetailFailuresAreReportedWithoutHidingResults(t *testing.T) {
	provider := avsubReviewProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search_results.php":
			io.WriteString(w, `<h5><a href="/movie1/abp-123">ABP-123</a></h5>`)
		case "/movie1/abp-123":
			io.WriteString(w, `<table id="subtitles_list"><tr><td>
			<a href="/movie1/abp-123/subtitles/zh/1">Failed</a>
			<a href="/movie1/abp-123/subtitles/zh/2">Working</a></td></tr></table>`)
		case "/movie1/abp-123/subtitles/zh/1":
			w.WriteHeader(http.StatusServiceUnavailable)
		case "/movie1/abp-123/subtitles/zh/2":
			io.WriteString(w, avsubDetailFixture("ABP-123.chs.srt"))
		default:
			t.Errorf("unexpected request %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	candidates, err := provider.Search(t.Context(), "ABP-123")
	if len(candidates) != 1 || err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("partial results=%+v error=%v", candidates, err)
	}
	finder := NewFinder(nil, WithProviders(provider))
	if candidates := finder.Search(t.Context(), "ABP-123", false); len(candidates) != 1 {
		t.Fatalf("failed detail hid usable results: %+v", candidates)
	}
}

func TestAVSubtitlesFailedDetailsDoNotLookLikeMissingSubtitles(t *testing.T) {
	for _, stage := range []string{"movie", "subtitle"} {
		t.Run(stage, func(t *testing.T) {
			provider := avsubReviewProvider(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/search_results.php":
					io.WriteString(w, `<h5><a href="/movie1/abp-123">ABP-123</a></h5>`)
				case "/movie1/abp-123":
					if stage == "movie" {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					io.WriteString(w, `<table id="subtitles_list"><tr><td><a href="/movie1/abp-123/subtitles/zh/1">Subtitles</a></td></tr></table>`)
				default:
					w.WriteHeader(http.StatusServiceUnavailable)
				}
			})
			service := NewService(nil, NewFinder(nil, WithProviders(provider)))
			config := DefaultConfig()
			result, err := service.TestDownload(t.Context(), TestRequest{Code: "ABP-123", Config: &config})
			if err != nil || result.Status != "failed" || !strings.Contains(strings.Join(result.Errors, " "), "503") {
				t.Fatalf("failed detail reported as no subtitles: %+v, %v", result, err)
			}
		})
	}
}

func avsubDetailFixture(filename string) string {
	return fmt.Sprintf(`<form action="/download_page.php"><input name="subid" value="1"><input name="revid" value="20261031"></form>
	<div><div class="subtitles_content_label">Archive content:</div><span class="text-mono">%s</span></div>`, filename)
}

func TestAVSubtitlesRejectsOtherMovieLinksAndAttachments(t *testing.T) {
	links := parseAVSubtitlesChineseLinks([]byte(`<table id="subtitles_list"><tr><td>
		<a href="/movie1/abp-123/subtitles/zh/1">Correct</a>
		<a href="/movie1/canonical-slug/subtitles/zh/3">Same movie after a slug change</a>
		<a href="/movie1/abp-123/subtitles/en/4">English</a>
		<a href="/movie2/other-999/subtitles/zh/2">Other movie</a></td></tr></table>`), "https://www.avsubtitles.com/movie1/abp-123")
	if len(links) != 2 || !strings.Contains(links[0], "/movie1/") || !strings.Contains(links[1], "/movie1/") {
		t.Fatalf("other movie entered subtitle links: %v", links)
	}
	for _, filename := range []string{"ABP-123.chs.srt", "chinese.srt", "OTHER-999.srt"} {
		_, ok := parseAVSubtitlesDetail([]byte(avsubDetailFixture(filename)), "ABP-123", defaultAVSubtitlesOrigin)
		if ok != (filename != "OTHER-999.srt") {
			t.Errorf("attachment %q accepted=%v", filename, ok)
		}
	}
}

func TestSubtitleCatDetailFailureIsReported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index.php" {
			io.WriteString(w, `<a href="/subs/123/movie.html">ABP-123</a>`)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()
	proxy, err := netx.NewProxyManager(netx.ProxyConfig{Enabled: true, URL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	provider := NewSubtitleCatProvider(proxy)
	provider.origin = "http://93.184.216.34"
	_, err = provider.Search(context.Background(), "ABP-123")
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("detail error was swallowed: %v", err)
	}
}
