package strm

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ppxb/miyabi/internal/export"
	"github.com/ppxb/miyabi/internal/netx"
)

const embyPlaybackInfoLimit = 1 << 20

var embyAuthToken = regexp.MustCompile(`(?i)(?:^|[,\s])Token="([^"]+)"`)

type embyPlaybackKey struct {
	token                [32]byte
	item, session, media string
	userAgent, clientIP  string
}

type embyStreamRead struct {
	playback   embyPlaybackKey
	uri        [32]byte
	nativeOnly bool
}

type embyProxy struct {
	native          *httputil.ReverseProxy
	resolver, media *http.Client
	miyabi          *url.URL
	trusted         []netip.Prefix
	now             func() time.Time
	mu              sync.Mutex
	sources         map[embyPlaybackKey]*embyProxySource
}

// NewEmbyProxy serves eligible Miyabi byte ranges on the Emby origin after a
// native playback check. Forwarding headers are accepted only from trusted peers.
func NewEmbyProxy(embyOrigin, miyabiBase string, trustedPeers ...string) (http.Handler, error) {
	emby, err := url.Parse(strings.TrimSpace(embyOrigin))
	if err != nil || !proxyHTTPURL(emby) || (emby.Path != "" && emby.Path != "/") || emby.RawQuery != "" || emby.ForceQuery {
		return nil, errors.New("Emby origin must be an HTTP(S) address without a path or query")
	}
	miyabi, err := url.Parse(strings.TrimSpace(miyabiBase))
	if err != nil || !proxyHTTPURL(miyabi) || miyabi.RawQuery != "" || miyabi.ForceQuery {
		return nil, errors.New("Miyabi base must match the HTTP(S) address in the exported STRM files")
	}
	trusted, err := parseEmbyProxies(trustedPeers)
	if err != nil {
		return nil, err
	}
	transport := netx.NewTransport(nil)
	mediaTransport := netx.NewSafeTransport(nil)
	mediaTransport.ResponseHeaderTimeout = 15 * time.Second
	p := &embyProxy{
		miyabi:  miyabi,
		trusted: trusted,
		now:     time.Now,
		sources: make(map[embyPlaybackKey]*embyProxySource),
		media: &http.Client{
			Transport: mediaTransport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= netx.MaxRedirects || !proxyHTTPURL(req.URL) {
					return errors.New("invalid media redirect")
				}
				req.Header.Del("Referer")
				return nil
			},
		},
		resolver: &http.Client{
			Transport:     transport,
			Timeout:       8 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
	p.native = &httputil.ReverseProxy{
		Transport: transport,
		Rewrite: func(req *httputil.ProxyRequest) {
			req.SetURL(emby)
			p.setForwarded(req)
			if embyPlaybackItem(req.In) != "" {
				req.Out.Header.Set("Accept-Encoding", "identity")
			}
		},
		ModifyResponse: p.observe,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "Emby upstream unavailable", http.StatusBadGateway)
		},
	}
	return p, nil
}

func proxyHTTPURL(u *url.URL) bool {
	return u != nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil && u.Fragment == ""
}

func (p *embyProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if read, ok := p.originalRead(r); ok {
		if source := p.source(read.playback); source != nil {
			p.serveRange(w, r, read, source)
			return
		}
	}
	p.native.ServeHTTP(w, r)
}

func embyRoute(path string) []string {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) > 0 && strings.EqualFold(parts[0], "emby") {
		return parts[1:]
	}
	return parts
}

func embyPlaybackItem(r *http.Request) string {
	parts := embyRoute(r.URL.Path)
	if (r.Method == http.MethodGet || r.Method == http.MethodPost) &&
		len(parts) == 3 && strings.EqualFold(parts[0], "Items") && strings.EqualFold(parts[2], "PlaybackInfo") &&
		!strings.EqualFold(r.URL.Query().Get("IsPlayback"), "false") {
		return parts[1]
	}
	return ""
}

func (p *embyProxy) originalRead(r *http.Request) (embyStreamRead, bool) {
	parts := embyRoute(r.URL.Path)
	ranges := r.Header.Values("Range")
	if r.Method != http.MethodGet || len(ranges) == 0 || !strings.HasPrefix(ranges[0], "bytes=") ||
		len(parts) != 3 || !strings.EqualFold(parts[0], "Videos") || parts[1] == "" {
		return embyStreamRead{}, false
	}
	for _, name := range []string{"Authorization", "X-Emby-Authorization", "X-Emby-Token", "X-MediaBrowser-Token"} {
		if len(r.Header.Values(name)) != 0 {
			return embyStreamRead{}, false
		}
	}
	name, _, _ := strings.Cut(strings.ToLower(parts[2]), ".")
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || name != "original" && (name != "stream" || !strings.EqualFold(embyParam(query, "Static"), "true")) {
		return embyStreamRead{}, false
	}
	origin := p.requestOrigin(r)
	key, ok := embyKey(r, parts[1], embyParam(query, "PlaySessionId"), embyParam(query, "MediaSourceId"), embyParam(query, "api_key"), origin.ip)
	scope := strings.Join([]string{r.URL.RequestURI(), origin.scheme, origin.host, r.Header.Get("Origin"), strings.Join(r.Header.Values("Cookie"), ";")}, "\x00")
	nativeOnly := len(ranges) != 1 || strings.Contains(ranges[0], ",")
	for _, name := range []string{"If-Range", "If-Match", "If-None-Match", "If-Modified-Since", "If-Unmodified-Since"} {
		if r.Header.Get(name) != "" {
			nativeOnly = true
		}
	}
	return embyStreamRead{playback: key, uri: sha256.Sum256([]byte(scope)), nativeOnly: nativeOnly}, ok
}

func embyParam(query url.Values, name string) string {
	var values []string
	for candidate, group := range query {
		if strings.EqualFold(candidate, name) {
			values = append(values, group...)
		}
	}
	if len(values) != 1 {
		return ""
	}
	return values[0]
}

func embyKey(r *http.Request, item, session, media, token, clientIP string) (embyPlaybackKey, bool) {
	if token == "" || session == "" || media == "" || clientIP == "" {
		return embyPlaybackKey{}, false
	}
	return embyPlaybackKey{token: sha256.Sum256([]byte(token)), item: item, session: session, media: media, userAgent: r.UserAgent(), clientIP: clientIP}, true
}

func embyRequestToken(r *http.Request) string {
	candidates := append([]string{}, r.URL.Query()["api_key"]...)
	candidates = append(candidates, r.Header.Values("X-Emby-Token")...)
	candidates = append(candidates, r.Header.Values("X-MediaBrowser-Token")...)
	for _, name := range []string{"Authorization", "X-Emby-Authorization"} {
		header := r.Header.Get(name)
		scheme, _, _ := strings.Cut(header, " ")
		if strings.EqualFold(scheme, "Emby") || strings.EqualFold(scheme, "MediaBrowser") {
			if match := embyAuthToken.FindStringSubmatch(header); match != nil {
				candidates = append(candidates, match[1])
			}
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	token := candidates[0]
	for _, candidate := range candidates[1:] {
		if token != candidate {
			return ""
		}
	}
	return token
}

func (p *embyProxy) observe(resp *http.Response) error {
	r := resp.Request
	if item := embyPlaybackItem(r); item != "" && resp.StatusCode == http.StatusOK {
		if token := embyRequestToken(r); token != "" {
			p.observePlaybackInfo(r, item, token, resp)
		}
	}
	return nil
}

func (p *embyProxy) observePlaybackInfo(r *http.Request, item, token string, resp *http.Response) {
	body := resp.Body
	data, err := io.ReadAll(io.LimitReader(body, embyPlaybackInfoLimit+1))
	// Keep the original PlaybackInfo response intact, including larger or
	// unfamiliar responses that cannot be used to seed the proxy.
	resp.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(data), body), body}
	if err != nil || len(data) > embyPlaybackInfoLimit {
		return
	}
	var playback struct {
		PlaySessionID string `json:"PlaySessionId"`
		MediaSources  []struct {
			ID                  string            `json:"Id"`
			Path                string            `json:"Path"`
			Protocol            string            `json:"Protocol"`
			SupportsDirectPlay  bool              `json:"SupportsDirectPlay"`
			IsInfiniteStream    bool              `json:"IsInfiniteStream"`
			RequiredHTTPHeaders map[string]string `json:"RequiredHttpHeaders"`
		} `json:"MediaSources"`
	}
	if json.Unmarshal(data, &playback) != nil {
		return
	}
	for _, source := range playback.MediaSources {
		key, ok := embyKey(r, item, playback.PlaySessionID, source.ID, token, r.Header.Get("X-Real-IP"))
		if !ok {
			continue
		}
		if !source.SupportsDirectPlay || source.IsInfiniteStream || !strings.EqualFold(source.Protocol, "Http") ||
			len(source.RequiredHTTPHeaders) != 0 || !p.isMiyabiSTRM(source.Path) {
			source.Path = ""
		}
		p.rememberSource(key, source.Path)
	}
}

func (p *embyProxy) isMiyabiSTRM(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || !proxyHTTPURL(u) || u.Scheme != p.miyabi.Scheme || !strings.EqualFold(u.Host, p.miyabi.Host) {
		return false
	}
	prefix := strings.TrimRight(p.miyabi.Path, "/") + export.STRMPlayPath
	relative, ok := strings.CutPrefix(u.Path, prefix)
	return ok && relative != "" && path.Clean(u.Path) == u.Path
}

func (p *embyProxy) rememberSource(key embyPlaybackKey, raw string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if raw == "" {
		delete(p.sources, key)
		return
	}
	now := p.now()
	if entry, found := p.sources[key]; found {
		if entry.strmURL == raw {
			entry.lastUse = now
			return
		}
	} else if len(p.sources) >= streamCacheLimit {
		var oldest embyPlaybackKey
		var lastUse time.Time
		for candidate, entry := range p.sources {
			if lastUse.IsZero() || entry.lastUse.Before(lastUse) {
				oldest, lastUse = candidate, entry.lastUse
			}
		}
		delete(p.sources, oldest)
	}
	p.sources[key] = &embyProxySource{strmURL: raw, lastUse: now, gate: make(chan struct{}, 1)}
}

func (p *embyProxy) source(key embyPlaybackKey) *embyProxySource {
	p.mu.Lock()
	defer p.mu.Unlock()
	source := p.sources[key]
	if source != nil {
		source.lastUse = p.now()
	}
	return source
}
