package subtitle

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	subtitlemeta "github.com/ppxb/miyabi/internal/domain/subtitle"
	"github.com/ppxb/miyabi/internal/netx"
)

func subtitleZIP(t *testing.T, name, body string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	file, err := writer.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(file, body); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestAVSubtitlesSearchAndDownload(t *testing.T) {
	const origin = "http://93.184.216.34"
	const filename = "ABP-123.1.www-avsubtitles-com++BOT++.zh.srt"
	archive := subtitleZIP(t, filename, traditionalSRT)
	var downloads int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search_results.php":
			if r.URL.Query().Get("search") != "ABP-123" || r.URL.Query().Get("language") != "zh" {
				t.Errorf("search query = %s", r.URL.RawQuery)
			}
			io.WriteString(w, `<a href="/movie1/abp-123">Cover</a>
				<h5><a href="/movie2/abp-1234">ABP-1234</a></h5>
				<h5><a href="/movie1/abp-123">[ABP-123] - Example (2020)</a></h5>
				<h5><a href="/movie1/abp-123">Duplicate</a></h5>
				<h5><a href="https://example.com/movie3/abp-123">ABP-123</a></h5>`)
		case "/movie1/abp-123":
			io.WriteString(w, `<a href="/movie1/abp-123/subtitles/zh/99">Outside subtitle table</a>
				<table id="subtitles_list"><tr><td>English</td><td>
				<a href="/movie1/abp-123/subtitles/en/6">Subtitles details</a></td></tr>
				<tr><td><img src="/icons/language_zh.svg">Chinese</td><td>
				<a href="/movie1/abp-123/subtitles/zh/7">Subtitles details</a>
				<a href="/movie1/abp-123/subtitles/zh/7">Duplicate</a>
				<a href="https://example.com/subtitles/zh/8">External</a></td></tr></table>`)
		case "/movie1/abp-123/subtitles/zh/7":
			io.WriteString(w, `<form method="get" action="/download_page.php">
				<input type="hidden" name="subid" value="7">
				<input type="hidden" name="revid" value="20261031120000"></form>
				<div class="subtitles_content_area"><div class="subtitles_content_label">Archive content:</div>
				<div class="subtitles_content_value"><span class="text-mono">README.txt</span>
				<span class="text-mono">`+filename+`</span></div></div>`)
		case "/download_page.php":
			if r.URL.Query().Get("subid") != "7" || r.URL.Query().Get("revid") != "20261031120000" {
				t.Errorf("download query = %s", r.URL.RawQuery)
			}
			http.SetCookie(w, &http.Cookie{Name: "PHPSESSID", Value: "anonymous-test-session", Path: "/"})
			io.WriteString(w, `<a href="https://example.com/ad.zip">Advertisement</a>
				<a id="bigbutton" href="./download_sub.php?subid=7&amp;revid=20261031120000">Download</a>`)
		case "/download_sub.php":
			cookie, err := r.Cookie("PHPSESSID")
			if err != nil || cookie.Value != "anonymous-test-session" {
				t.Error("download lost anonymous session")
				w.WriteHeader(http.StatusForbidden)
				return
			}
			downloads++
			w.Header().Set("Content-Type", "application/zip")
			w.Write(archive)
		default:
			t.Errorf("unexpected request: %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	proxy, err := netx.NewProxyManager(netx.ProxyConfig{Enabled: true, URL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	provider := NewAVSubtitlesProvider(proxy)
	provider.origin = origin
	candidates, err := provider.Search(t.Context(), " ABP-123 ")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].Provider != "AVSubtitles" || candidates[0].Format != "srt" ||
		candidates[0].Language != subtitlemeta.LangUnknown || len(Rank(candidates, "ABP-123", false)) != 1 {
		t.Fatalf("candidates = %+v", candidates)
	}
	if downloads != 0 {
		t.Fatal("search downloaded an archive before a track was selected")
	}
	candidates[0].download.(*avsubtitlesDownload).delay = 0
	finder := NewFinder(proxy, WithProviders(provider))
	body, language, err := finder.Download(t.Context(), candidates[0])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, append(bytes.Clone(utf8BOM), traditionalSRT...)) || language != subtitlemeta.LangTraditionalChinese || downloads != 1 {
		t.Fatalf("download language = %s, count = %d, body = %q", language, downloads, body)
	}
}

func TestAVSubtitlesRejectsInvalidDownloads(t *testing.T) {
	const filename = "ABP-123.srt"
	for _, test := range []struct {
		name string
		page string
		body []byte
		want string
	}{
		{name: "missing link", page: `<html>Unavailable</html>`, want: "link missing"},
		{name: "external link", page: `<a id="bigbutton" href="https://example.com/file.zip">Download</a>`, want: "link missing"},
		{name: "error page", body: []byte(`<html>Error 403</html>`), want: "archive:"},
		{name: "missing file", body: subtitleZIP(t, "other.srt", simplifiedSRT), want: "archive missing"},
		{name: "oversized file", body: subtitleZIP(t, filename, strings.Repeat("x", maxSize+1)), want: "size limit"},
		{name: "invalid subtitle", body: subtitleZIP(t, filename, "Not a timed subtitle"), want: "no timed cues"},
	} {
		t.Run(test.name, func(t *testing.T) {
			finder := NewFinder(nil, WithProviders())
			finder.client.Transport = subtitleTransport(func(req *http.Request) (*http.Response, error) {
				body := test.body
				if req.URL.Path == "/download_page.php" {
					page := test.page
					if page == "" {
						page = `<a id="bigbutton" href="/download_sub.php">Download</a>`
					}
					body = []byte(page)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header), Request: req}, nil
			})
			_, _, err := finder.Download(t.Context(), Candidate{
				URL: "http://93.184.216.34/download_page.php", Format: "srt",
				download: &avsubtitlesDownload{filename: filename},
			})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("download error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestAVSubtitlesCountdownCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finder := NewFinder(nil, WithProviders())
	finder.client.Transport = subtitleTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/download_page.php" {
			t.Error("canceled download fetched the archive")
		}
		cancel()
		return &http.Response{
			StatusCode: http.StatusOK, Header: make(http.Header), Request: req,
			Body: io.NopCloser(strings.NewReader(`<a id="bigbutton" href="/download_sub.php">Download</a>`)),
		}, nil
	})
	_, _, err := finder.Download(ctx, Candidate{
		URL: "http://93.184.216.34/download_page.php", Format: "srt",
		download: &avsubtitlesDownload{filename: "ABP-123.srt", delay: time.Hour},
	})
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("canceled download error = %v", err)
	}
}

func TestAVSubtitlesFailurePreservesExistingSources(t *testing.T) {
	var avsubRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/xunlei":
			io.WriteString(w, `{"code":0,"data":[{"name":"ABP-123.srt","url":"https://example.com/xunlei.srt","ext":"srt","languages":["zh-CN"]}]}`)
		case "/index.php":
			io.WriteString(w, `<a href="/subs/123/movie.html">ABP-123</a>`)
		case "/subs/123/movie.html":
			io.WriteString(w, `<div class="sub-single"><img class="flag" alt="zh-TW">
				<a class="green-link" href="https://example.com/cat.srt">Download</a></div>`)
		case "/search_results.php":
			avsubRequests.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			t.Errorf("unexpected request: %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
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
	candidates := finder.Search(t.Context(), "ABP-123", false)
	if avsubRequests.Load() != 1 || len(candidates) != 2 || candidates[0].Provider != "迅雷" || candidates[1].Provider != "SubtitleCat" {
		t.Fatalf("existing sources after AVSubtitles failure = %+v", candidates)
	}
}
