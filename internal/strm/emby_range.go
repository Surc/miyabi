package strm

import (
	"context"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ppxb/miyabi/internal/domain"
)

const embyFailureBackoff = 5 * time.Second

type embyGrant struct {
	uri    [32]byte
	status int
	until  time.Time
	header http.Header
	size   string
}

type embyProxySource struct {
	strmURL string
	lastUse time.Time // protected by the proxy's map lock

	// Serialize native authorization and link resolution for this playback.
	// Each reader streams its own bytes after releasing the gate.
	gate       chan struct{}
	grant      embyGrant
	link       cachedURL
	retryUntil time.Time
}

func (s *embyProxySource) enter(ctx context.Context) bool {
	select {
	case s.gate <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func (s *embyProxySource) leave() { <-s.gate }

func (p *embyProxy) serveRange(w http.ResponseWriter, r *http.Request, read embyStreamRead, s *embyProxySource) {
	if !s.enter(r.Context()) {
		return
	}
	if p.source(read.playback) != s {
		s.leave()
		p.native.ServeHTTP(w, r)
		return
	}
	now := p.now()
	if s.throttled(w, read, now) {
		s.leave()
		return
	}
	grant := s.grant
	if read.nativeOnly || grant.uri != read.uri || !now.Before(grant.until) {
		p.nativeRange(w, r, read, s)
		return
	}
	if grant.status != http.StatusPartialContent {
		s.leave()
		copyEmbyHeaders(w.Header(), grant.header, false)
		w.Header().Set("Cache-Control", "private, no-store")
		http.Error(w, http.StatusText(grant.status), grant.status)
		return
	}
	if !now.Before(s.link.expires) {
		link, retryUntil := p.resolve(r.Context(), s.strmURL, r.UserAgent())
		if r.Context().Err() != nil {
			s.leave()
			return
		}
		s.link = link
		s.retryUntil = retryUntil
		if s.throttled(w, read, p.now()) {
			s.leave()
			return
		}
		if link.address == "" {
			s.link.expires = p.now().Add(embyFailureBackoff)
		}
	}
	if s.link.address == "" || !p.now().Before(grant.until) {
		p.nativeRange(w, r, read, s)
		return
	}
	link := s.link
	s.leave()
	served, retryUntil := p.streamRange(w, r, grant, link.address)
	if served || r.Context().Err() != nil || !s.enter(r.Context()) {
		return
	}
	if retryUntil.After(s.retryUntil) {
		s.retryUntil = retryUntil
	}
	if s.throttled(w, read, p.now()) {
		s.leave()
		return
	}
	// Native Emby may still be able to read a link rejected by the CDN.
	// A late failure must not discard a newer link.
	if s.link == link {
		s.link = cachedURL{expires: p.now().Add(embyFailureBackoff)}
	}
	p.nativeRange(w, r, read, s)
}

// Release waiting readers as soon as Emby authenticates the original request,
// even if its first range has an open end or the player cancels the body.
func (p *embyProxy) nativeRange(w http.ResponseWriter, r *http.Request, read embyStreamRead, s *embyProxySource) {
	writer := &embyHeaderWriter{ResponseWriter: w}
	writer.observe = func(status int) {
		defer s.leave()
		if read.nativeOnly && status != http.StatusTooManyRequests {
			return
		}
		now := p.now()
		header := w.Header()
		duration := embyFailureBackoff
		switch status {
		case http.StatusPartialContent:
			if mediaSize(header) == "" {
				s.grant = embyGrant{}
				return
			}
			if s.grant.uri == read.uri && s.grant.status == status && now.Before(s.grant.until) {
				return
			}
			duration = streamCacheTTL
		case http.StatusTooManyRequests:
			s.retryUntil = embyRetryUntil(header, now)
			duration = s.retryUntil.Sub(now)
			if header.Get("Retry-After") == "" {
				header.Set("Retry-After", retrySeconds(duration))
			}
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
			http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			if r.Context().Err() != nil {
				return
			}
		default:
			s.grant = embyGrant{}
			return
		}
		saved := make(http.Header)
		copyEmbyHeaders(saved, header, status == http.StatusPartialContent)
		s.grant = embyGrant{uri: read.uri, status: status, until: now.Add(duration), header: saved, size: mediaSize(header)}
	}
	defer func() {
		if writer.observe != nil {
			s.leave()
		}
	}()
	p.native.ServeHTTP(writer, r)
}

type embyHeaderWriter struct {
	http.ResponseWriter
	observe func(int)
}

func (w *embyHeaderWriter) WriteHeader(status int) {
	if status >= 200 && w.observe != nil {
		observe := w.observe
		w.observe = nil
		observe(status)
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *embyHeaderWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func embyRetryUntil(header http.Header, now time.Time) time.Time {
	wait := domain.ParseRetryAfter(header.Get("Retry-After"), now)
	if wait <= 0 {
		wait = embyFailureBackoff
	}
	return now.Add(wait)
}

func retrySeconds(wait time.Duration) string {
	return strconv.FormatInt(int64(math.Ceil(wait.Seconds())), 10)
}

func (s *embyProxySource) throttled(w http.ResponseWriter, read embyStreamRead, now time.Time) bool {
	if !now.Before(s.retryUntil) {
		return false
	}
	if s.grant.uri == read.uri {
		copyEmbyHeaders(w.Header(), s.grant.header, false)
	}
	w.Header().Set("Retry-After", retrySeconds(s.retryUntil.Sub(now)))
	w.Header().Set("Cache-Control", "private, no-store")
	http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
	return true
}

func (p *embyProxy) resolve(ctx context.Context, source, agent string) (cachedURL, time.Time) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return cachedURL{}, time.Time{}
	}
	req.Header.Set("User-Agent", agent)
	resp, err := p.resolver.Do(req)
	if err != nil {
		return cachedURL{}, time.Time{}
	}
	defer resp.Body.Close()
	now := p.now()
	if resp.StatusCode == http.StatusTooManyRequests {
		return cachedURL{}, embyRetryUntil(resp.Header, now)
	}
	raw := resp.Header.Get("Location")
	target, err := url.Parse(raw)
	expires := streamCacheExpiry(raw, now)
	if resp.StatusCode != http.StatusFound || err != nil || !proxyHTTPURL(target) || raw == source || !now.Before(expires) {
		return cachedURL{}, time.Time{}
	}
	return cachedURL{address: raw, expires: expires}, time.Time{}
}

// Only commit the player response once the CDN has returned a usable range.
func (p *embyProxy) streamRange(w http.ResponseWriter, r *http.Request, grant embyGrant, address string) (bool, time.Time) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, address, nil)
	if err != nil {
		return false, time.Time{}
	}
	req.Header.Set("User-Agent", r.UserAgent())
	req.Header.Set("Range", r.Header.Get("Range"))
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := p.media.Do(req)
	if err != nil {
		return false, time.Time{}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return false, embyRetryUntil(resp.Header, p.now())
	}
	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable && resp.Header.Get("Content-Range") == "bytes */"+grant.size {
		copyEmbyHeaders(w.Header(), grant.header, false)
		w.Header().Set("Content-Range", resp.Header.Get("Content-Range"))
		w.WriteHeader(resp.StatusCode)
		return true, time.Time{}
	}
	encoding := resp.Header.Get("Content-Encoding")
	if resp.StatusCode != http.StatusPartialContent || !matchesEmbyRange(r.Header.Get("Range"), resp, grant.size) || (encoding != "" && encoding != "identity") {
		return false, time.Time{}
	}
	copyEmbyHeaders(w.Header(), grant.header, true)
	for _, name := range []string{"Content-Length", "Content-Range", "Accept-Ranges"} {
		if value := resp.Header.Get(name); value != "" {
			w.Header().Set(name, value)
		}
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		panic(http.ErrAbortHandler)
	}
	return true, time.Time{}
}

func mediaSize(header http.Header) string {
	value, ok := strings.CutPrefix(header.Get("Content-Range"), "bytes ")
	_, size, found := strings.Cut(value, "/")
	length, err := strconv.ParseInt(size, 10, 64)
	if !ok || !found || err != nil || length <= 0 {
		return ""
	}
	return size
}

func matchesEmbyRange(requested string, resp *http.Response, size string) bool {
	if mediaSize(resp.Header) != size {
		return false
	}
	total, _ := strconv.ParseInt(size, 10, 64)
	span, _, _ := strings.Cut(strings.TrimPrefix(resp.Header.Get("Content-Range"), "bytes "), "/")
	left, right, _ := strings.Cut(span, "-")
	start, errStart := strconv.ParseInt(left, 10, 64)
	end, errEnd := strconv.ParseInt(right, 10, 64)
	if errStart != nil || errEnd != nil || start < 0 || end < start || end >= total ||
		(resp.ContentLength >= 0 && resp.ContentLength != end-start+1) {
		return false
	}
	left, right, ok := strings.Cut(strings.TrimPrefix(requested, "bytes="), "-")
	if !ok {
		return false
	}
	if left == "" {
		suffix, err := strconv.ParseInt(right, 10, 64)
		return err == nil && suffix > 0 && start == max(0, total-suffix)
	}
	offset, err := strconv.ParseInt(left, 10, 64)
	if err != nil || offset != start {
		return false
	}
	if right == "" {
		return true
	}
	limit, err := strconv.ParseInt(right, 10, 64)
	return err == nil && end <= limit
}

func copyEmbyHeaders(dst, src http.Header, media bool) {
	for _, name := range []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Credentials", "Access-Control-Expose-Headers",
		"Timing-Allow-Origin", "Vary", "WWW-Authenticate"} {
		for _, value := range src.Values(name) {
			dst.Add(name, value)
		}
	}
	if media {
		for _, name := range []string{"Content-Type", "Content-Disposition", "ETag", "Last-Modified"} {
			if value := src.Get(name); value != "" {
				dst.Set(name, value)
			}
		}
	}
}
