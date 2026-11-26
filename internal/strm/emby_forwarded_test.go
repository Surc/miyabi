package strm

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmbyProxyForwardsTrustedClientOrigin(t *testing.T) {
	emby := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, name := range []string{"X-Real-IP", "X-Forwarded-For", "X-Forwarded-Proto", "X-Forwarded-Host"} {
			w.Header().Set(name, r.Header.Get(name))
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(emby.Close)
	handler, err := NewEmbyProxy(emby.URL, emby.URL, "10.0.0.0/24", "::1")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, peer, chain, proto, host, wantIP, wantProto, wantHost string
	}{
		{"direct", "192.0.2.7:4100", "203.0.113.5", "https", "public.example", "192.0.2.7", "http", "listener.example"},
		{"frontend", "[::1]:4100", "203.0.113.5", "https", "public.example:443", "203.0.113.5", "https", "public.example:443"},
		{"trusted chain", "10.0.0.1:4100", "203.0.113.5, 10.0.0.2", "https, http", "public.example, inside.example", "203.0.113.5", "https", "public.example"},
		{"client boundary", "10.0.0.1:4100", "198.51.100.1, 203.0.113.5", "https, http", "first.example, public.example", "203.0.113.5", "http", "public.example"},
		{"real IP only", "10.0.0.1:4100", "", "https", "public.example", "198.51.100.8", "https", "public.example"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/emby/System/Info/Public", nil)
			r.Host, r.RemoteAddr = "listener.example", tc.peer
			r.Header.Set("X-Real-IP", "198.51.100.8")
			r.Header.Set("X-Forwarded-For", tc.chain)
			r.Header.Set("X-Forwarded-Proto", tc.proto)
			r.Header.Set("X-Forwarded-Host", tc.host)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != 204 || w.Header().Get("X-Real-IP") != tc.wantIP || w.Header().Get("X-Forwarded-For") != tc.wantIP ||
				w.Header().Get("X-Forwarded-Proto") != tc.wantProto || w.Header().Get("X-Forwarded-Host") != tc.wantHost {
				t.Fatalf("forwarded origin = %d %v", w.Code, w.Header())
			}
		})
	}
	if _, err := NewEmbyProxy(emby.URL, emby.URL, "invalid"); err == nil {
		t.Fatal("invalid trusted peer accepted")
	}
}
