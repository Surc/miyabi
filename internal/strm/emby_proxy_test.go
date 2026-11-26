package strm

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testEmbyAgent  = "Emby Web test player"
	testEmbyToken  = "test-user-token"
	nativeAgent    = "Emby media server fixture"
	testEmbyStream = "/emby/videos/123/original.mp4?api_key=" + testEmbyToken + "&DeviceId=device&MediaSourceId=source&PlaySessionId=playback"
)

type embyProxyFixture struct {
	handler *embyProxy
	video   string
	clock   atomic.Int64

	nativeReads     atomic.Int32
	nativeLookups   atomic.Int32
	resolverLookups atomic.Int32
	mediaReads      atomic.Int32

	nativeDenied    atomic.Bool
	nativeLimited   atomic.Bool
	resolverLimited atomic.Bool
	cdnStatus       atomic.Int32
	wrongOffset     atomic.Bool
	retryAfter      string
	nativeBody      func(*http.Request)
}

func newEmbyProxyFixture(t *testing.T, sourceURL func(string) string) *embyProxyFixture {
	t.Helper()
	f := &embyProxyFixture{video: strings.Repeat("0123456789abcdef", 64), retryAfter: "90"}
	f.clock.Store(1800000000)
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "" ||
			r.Header.Get("X-Emby-Token") != "" || r.Header.Get("Origin") != "" || r.URL.Query().Get("api_key") != "" {
			t.Error("user or server credentials leaked into the media request")
		}
		if r.URL.Path == "/entry" {
			http.Redirect(w, r, "/video", http.StatusTemporaryRedirect)
			return
		}
		if r.UserAgent() != nativeAgent {
			f.mediaReads.Add(1)
			if r.UserAgent() != testEmbyAgent || r.Header.Get("Range") == "" || r.Header.Get("Accept-Encoding") != "identity" {
				t.Error("unexpected media headers")
			}
			if code := f.cdnStatus.Load(); code != 0 {
				w.Header().Set("Retry-After", f.retryAfter)
				w.WriteHeader(int(code))
				return
			}
			if f.wrongOffset.Load() {
				w.Header().Set("Content-Range", "bytes 0-2/1024")
				w.WriteHeader(http.StatusPartialContent)
				_, _ = io.WriteString(w, "bad")
				return
			}
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("ETag", "\"cdn-version\"")
		http.ServeContent(w, r, "video.mp4", time.Time{}, strings.NewReader(f.video))
	}))
	t.Cleanup(cdn.Close)
	miyabi := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/media/api/strm/play/video-101" || r.URL.Query().Get("token") != "strm-only" {
			t.Error("unexpected Miyabi URL or method")
		}
		limited := false
		if r.UserAgent() == nativeAgent {
			f.nativeLookups.Add(1)
			limited = f.nativeLimited.Load()
		} else {
			f.resolverLookups.Add(1)
			if r.UserAgent() != testEmbyAgent || r.Header.Get("Range") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("X-Emby-Token") != "" {
				t.Error("unexpected resolver headers")
			}
			limited = f.resolverLimited.Load()
		}
		if limited {
			if f.retryAfter != "" {
				w.Header().Set("Retry-After", f.retryAfter)
			}
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Location", fmt.Sprintf("%s/entry?t=%d", cdn.URL, f.clock.Load()+45))
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(miyabi.Close)
	miyabi.Client().CheckRedirect = func(r *http.Request, via []*http.Request) error {
		r.Header.Del("Referer")
		return nil
	}
	source := miyabi.URL + "/media/api/strm/play/video-101?token=strm-only"
	path := source
	if sourceURL != nil {
		path = sourceURL(source)
	}
	emby := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/emby/Items/123/PlaybackInfo":
			if r.Header.Get("X-Emby-Authorization") != "MediaBrowser Client=\"Emby Web\", Token=\""+testEmbyToken+"\"" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"PlaySessionId": "playback", "MediaSources": []map[string]any{
					{"Id": "source", "Path": path, "Protocol": "Http", "SupportsDirectPlay": true},
				},
			})
		case "/emby/videos/123/original.mp4", "/emby/videos/123/stream.mp4":
			f.nativeReads.Add(1)
			if f.nativeDenied.Load() || r.URL.Query().Get("api_key") != testEmbyToken {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			// Real native reads visit Miyabi and then follow the redirect to the CDN.
			req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, source, nil)
			req.Header.Set("User-Agent", nativeAgent)
			req.Header.Set("Range", r.Header.Get("Range"))
			resp, err := miyabi.Client().Do(req)
			if err != nil {
				if r.Context().Err() == nil {
					t.Error(err)
				}
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			defer resp.Body.Close()
			for _, name := range []string{"Content-Length", "Content-Range", "Accept-Ranges", "Retry-After"} {
				if v := resp.Header.Get(name); v != "" {
					w.Header().Set(name, v)
				}
			}
			w.Header().Set("Content-Type", "video/mp4")
			w.Header().Set("ETag", "\"emby-version\"")
			if origin := r.Header.Get("Origin"); origin != "" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
			}
			w.WriteHeader(resp.StatusCode)
			if f.nativeBody != nil {
				w.(http.Flusher).Flush()
				f.nativeBody(r)
			}
			_, _ = io.Copy(w, resp.Body)
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	t.Cleanup(emby.Close)
	handler, err := NewEmbyProxy(emby.URL, miyabi.URL+"/media")
	if err != nil {
		t.Fatal(err)
	}
	f.handler = handler.(*embyProxy)
	// The fixture CDN runs on loopback. Production uses the public-IP-checked transport.
	f.handler.media.Transport = cdn.Client().Transport
	f.handler.now = func() time.Time { return time.Unix(f.clock.Load(), 0) }
	return f
}

func (f *embyProxyFixture) playbackInfo(t *testing.T) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/emby/Items/123/PlaybackInfo?IsPlayback=true", nil)
	r.Header.Set("User-Agent", testEmbyAgent)
	r.Header.Set("X-Emby-Authorization", "MediaBrowser Client=\"Emby Web\", Token=\""+testEmbyToken+"\"")
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "\"PlaySessionId\":\"playback\"") {
		t.Fatalf("unexpected PlaybackInfo response %d", w.Code)
	}
}

func (f *embyProxyFixture) request(path string, offset int, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Header.Set("User-Agent", testEmbyAgent)
	r.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, offset+2))
	r.Header.Set("Origin", "https://player.example")
	r.Header.Set("Cookie", "emby=fixture")
	for name, value := range headers {
		r.Header.Set(name, value)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

func (f *embyProxyFixture) checkRange(t *testing.T, offset int) {
	t.Helper()
	w := f.request(testEmbyStream, offset, nil)
	if w.Code != 206 || w.Header().Get("Location") != "" || w.Body.String() != f.video[offset:offset+3] ||
		w.Header().Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", offset, offset+2, len(f.video)) ||
		w.Header().Get("Content-Type") != "video/mp4" || w.Header().Get("ETag") != "\"emby-version\"" ||
		w.Header().Get("Access-Control-Allow-Origin") != "https://player.example" {
		t.Errorf("range %d returned %d %q with headers %v", offset, w.Code, w.Body.String(), w.Header())
	}
}

func parallelEmbyReads(count int, read func(int)) {
	var wg sync.WaitGroup
	start := make(chan struct{})
	for n := 0; n < count; n++ {
		wg.Go(func() { <-start; read(n) })
	}
	close(start)
	wg.Wait()
}

func TestEmbyProxyConcurrentColdRanges(t *testing.T) {
	f := newEmbyProxyFixture(t, nil)
	f.playbackInfo(t)
	parallelEmbyReads(11, func(worker int) {
		for offset := worker; offset < 253; offset += 11 {
			f.checkRange(t, offset)
		}
	})
	if n, native, resolved, media := f.nativeReads.Load(), f.nativeLookups.Load(), f.resolverLookups.Load(), f.mediaReads.Load(); n != 1 || native != 1 || resolved != 1 || media != 252 {
		t.Fatalf("253 ranges: native requests=%d, Miyabi from Emby=%d, Miyabi from proxy=%d, proxied CDN ranges=%d", n, native, resolved, media)
	}
}

func TestEmbyProxyDoesNotWaitForFirstRangeBody(t *testing.T) {
	f := newEmbyProxyFixture(t, nil)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	f.nativeBody = func(r *http.Request) {
		if r.Header.Get("Range") == "bytes=0-2" {
			close(entered)
			<-release
		}
	}
	f.playbackInfo(t)
	first, second := make(chan struct{}), make(chan struct{})
	go func() { defer close(first); f.checkRange(t, 0) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first native range did not start")
	}
	go func() { defer close(second); f.checkRange(t, 42) }()
	select {
	case <-second:
	case <-time.After(5 * time.Second):
		t.Error("seek blocked on the first range body")
	}
	once.Do(func() { close(release) })
	<-first
	<-second
}

func TestEmbyProxyServesOpenAndSuffixRangesOnThePlayerOrigin(t *testing.T) {
	f := newEmbyProxyFixture(t, nil)
	proxy := httptest.NewServer(f.handler)
	t.Cleanup(proxy.Close)
	player := proxy.Client()
	player.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, _ := http.NewRequest(http.MethodPost, proxy.URL+"/emby/Items/123/PlaybackInfo", nil)
	req.Header.Set("X-Emby-Authorization", "MediaBrowser Client=\"Emby Web\", Token=\""+testEmbyToken+"\"")
	req.Header.Set("User-Agent", testEmbyAgent)
	info, err := player.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	info.Body.Close()
	address := proxy.URL + strings.Replace(testEmbyStream, "original.mp4", "stream.mp4", 1) + "&Static=true"
	for _, tc := range []struct{ requested, want string }{
		{"bytes=0-2", f.video[:3]}, {"bytes=991-", f.video[991:]}, {"bytes=-10", f.video[len(f.video)-10:]},
	} {
		req, _ := http.NewRequest(http.MethodGet, address, nil)
		req.Header.Set("User-Agent", testEmbyAgent)
		req.Header.Set("Range", tc.requested)
		response, err := player.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != 206 || response.Header.Get("Location") != "" || string(body) != tc.want {
			t.Fatalf("player range %s = %d, %d bytes, %v", tc.requested, response.StatusCode, len(body), err)
		}
	}
	if f.nativeLookups.Load() != 1 || f.resolverLookups.Load() != 1 || f.mediaReads.Load() != 2 {
		t.Fatal("actual HTTP requests did not share the signed link")
	}
}

func TestEmbyProxySharesRateLimits(t *testing.T) {
	for _, source := range []string{"native", "resolver", "CDN"} {
		t.Run(source, func(t *testing.T) {
			f := newEmbyProxyFixture(t, nil)
			f.retryAfter = time.Unix(f.clock.Load()+90, 0).UTC().Format(http.TimeFormat)
			f.playbackInfo(t)
			if source == "native" {
				f.nativeLimited.Store(true)
			} else {
				f.checkRange(t, 0)
				if source == "resolver" {
					f.resolverLimited.Store(true)
				} else {
					f.cdnStatus.Store(429)
				}
			}
			// For CDN throttling, receive the first 429 before the new batch of ranges.
			if source == "CDN" && f.request(testEmbyStream, 1, nil).Code != 429 {
				t.Fatal("expected CDN limit")
			}
			parallelEmbyReads(8, func(n int) {
				w := f.request(testEmbyStream, n, nil)
				hint := w.Header().Get("Retry-After")
				if w.Code != 429 || (hint != "90" && hint != f.retryAfter) {
					t.Errorf("throttle = %d, Retry-After=%q", w.Code, hint)
				}
			})
			f.clock.Add(61)
			w := f.request(testEmbyStream, 35, nil)
			if w.Code != 429 || w.Header().Get("Retry-After") != "29" {
				t.Errorf("throttle across reauthorization = %d %v", w.Code, w.Header())
			}
			if w := f.request(testEmbyStream, 35, map[string]string{"If-Range": "\"emby-version\""}); w.Code != 429 {
				t.Errorf("conditional read escaped cooldown: %d", w.Code)
			}
			wantResolver := int32(1)
			if source == "native" {
				wantResolver = 0
			}
			if f.nativeLookups.Load() != 1 || f.resolverLookups.Load() != wantResolver || f.mediaReads.Load() > 1 {
				t.Fatalf("throttle leaked requests: native=%d, resolver=%d, CDN=%d", f.nativeLookups.Load(), f.resolverLookups.Load(), f.mediaReads.Load())
			}
			f.nativeLimited.Store(false)
			f.resolverLimited.Store(false)
			f.cdnStatus.Store(0)
			f.clock.Add(29)
			parallelEmbyReads(8, func(n int) { f.checkRange(t, n) })
			if f.nativeReads.Load() != 2 {
				t.Errorf("renewal made %d total native reads", f.nativeReads.Load())
			}
		})
	}
}

func TestEmbyProxyLimitsWithoutRetryAfter(t *testing.T) {
	f := newEmbyProxyFixture(t, nil)
	f.retryAfter = ""
	f.nativeLimited.Store(true)
	f.playbackInfo(t)
	for _, want := range []string{"5", "4"} {
		w := f.request(testEmbyStream, 1, nil)
		if w.Code != 429 || w.Header().Get("Retry-After") != want {
			t.Fatalf("missing upstream hint: %d %v", w.Code, w.Header())
		}
		f.clock.Add(1)
	}
	if f.nativeLookups.Load() != 1 {
		t.Fatal("missing upstream hint caused repeated STRM reads")
	}
}

func TestEmbyProxyCDNFailureFallsBackBeforeWriting(t *testing.T) {
	f := newEmbyProxyFixture(t, nil)
	f.playbackInfo(t)
	f.checkRange(t, 1)
	f.cdnStatus.Store(403)
	f.checkRange(t, 7)
	f.checkRange(t, 42)
	if f.nativeReads.Load() != 3 || f.resolverLookups.Load() != 1 || f.mediaReads.Load() != 1 {
		t.Fatal("failed CDN link was reused or re-resolved")
	}
	f.cdnStatus.Store(0)
	f.clock.Add(6)
	f.checkRange(t, 43)
	if f.resolverLookups.Load() != 2 || f.nativeReads.Load() != 3 {
		t.Fatal("recovered CDN was not used")
	}
	w := f.request(testEmbyStream, len(f.video)+3, nil)
	if w.Code != 416 || w.Header().Get("Content-Range") != "bytes */"+strconv.Itoa(len(f.video)) {
		t.Fatalf("out of range: %d %v", w.Code, w.Header())
	}
}

func TestEmbyProxyRefreshesLinksAndNativePermissions(t *testing.T) {
	f := newEmbyProxyFixture(t, nil)
	f.playbackInfo(t)
	f.checkRange(t, 1)
	f.checkRange(t, 2)
	f.clock.Add(16)
	parallelEmbyReads(8, func(n int) { f.checkRange(t, n) })
	if f.nativeReads.Load() != 1 || f.resolverLookups.Load() != 2 {
		t.Fatal("expired CDN link did not refresh exactly once")
	}
	f.clock.Add(45)
	f.nativeDenied.Store(true)
	parallelEmbyReads(8, func(n int) {
		if w := f.request(testEmbyStream, n, nil); w.Code != 403 || w.Header().Get("Location") != "" {
			t.Errorf("revoked permission = %d", w.Code)
		}
	})
	if f.nativeReads.Load() != 2 || f.resolverLookups.Load() != 2 {
		t.Fatal("denied renewal repeated or exposed a cached link")
	}
}

func TestEmbyProxyDiscardsWrongCDNOffset(t *testing.T) {
	f := newEmbyProxyFixture(t, nil)
	f.playbackInfo(t)
	f.checkRange(t, 0)
	f.wrongOffset.Store(true)
	f.checkRange(t, 42)
	if f.nativeReads.Load() != 2 {
		t.Fatal("wrong byte offset was served to the player")
	}
}

func TestEmbyProxyKeepsNativeFallbacks(t *testing.T) {
	f := newEmbyProxyFixture(t, nil)
	f.playbackInfo(t)
	f.checkRange(t, 1)
	f.checkRange(t, 2)
	w := f.request(strings.Replace(testEmbyStream, testEmbyToken, "different", 1), 3, nil)
	if w.Code != 403 {
		t.Fatal("wrong credential used the cached approval")
	}
	for _, h := range []map[string]string{
		{"If-Range": "\"prior\""}, {"X-Emby-Token": testEmbyToken}, {"User-Agent": "different"}, {"Cookie": "other=1"}, {"Origin": "https://other.example"},
	} {
		before := f.nativeReads.Load()
		w := f.request(testEmbyStream, 4, h)
		if w.Code != 206 || f.nativeReads.Load() != before+1 {
			t.Fatalf("unmatched scope did not reach Emby: %d", w.Code)
		}
		if origin := h["Origin"]; origin != "" && w.Header().Get("Access-Control-Allow-Origin") != origin {
			t.Error("wrong CORS origin")
		}
	}
	other := newEmbyProxyFixture(t, func(s string) string { return strings.Replace(s, "/api/strm/play/", "/foreign/", 1) })
	other.playbackInfo(t)
	other.checkRange(t, 1)
	other.checkRange(t, 2)
	if other.nativeReads.Load() != 2 || other.resolverLookups.Load() != 0 {
		t.Fatal("unrelated media entered the cache")
	}
}

func TestEmbyProxyRecognizesCanonicalPlaybackPaths(t *testing.T) {
	const base = "https://miyabi.example/media"
	handler, err := NewEmbyProxy("https://emby.example", base)
	if err != nil {
		t.Fatal(err)
	}
	p := handler.(*embyProxy)
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"/api/strm/play/1001", true},
		{"/api/strm/play/115/1001", true},
		{"/api/strm/play/1001/Movie%20Name.mp4?token=fixture", true},
		{"/api/strm/play/115//1001", false},
		{"/api/strm/play/", false},
	} {
		if got := p.isMiyabiSTRM(base + tc.path); got != tc.want {
			t.Errorf("isMiyabiSTRM(%q) = %t, want %t", tc.path, got, tc.want)
		}
	}
}
