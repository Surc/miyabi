package api

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ppxb/miyabi/internal/catalogue"
	"github.com/ppxb/miyabi/internal/domain"
)

type archiveAPIStub struct {
	browseAPIStub
	code string
}

func (s *archiveAPIStub) ExportCache(_ context.Context, dst io.Writer) error {
	s.calls++
	_, err := io.WriteString(dst, "PK-archive")
	return err
}
func (s *archiveAPIStub) ImportCache(_ context.Context, src io.ReaderAt, size int64) (catalogue.BrowseCacheStats, error) {
	s.calls++
	data := make([]byte, size)
	_, _ = src.ReadAt(data, 0)
	s.code = string(data)
	return catalogue.BrowseCacheStats{Movies: 3, Images: 2, Bytes: size}, nil
}
func (s *archiveAPIStub) CachedRating(source, id string) *catalogue.BrowseCached[domain.ExternalRating] {
	s.calls++
	s.source, s.code = source, id
	return nil
}
func (s *archiveAPIStub) RatingByCode(_ context.Context, code string, refresh bool) (catalogue.BrowseCached[domain.ExternalRating], error) {
	s.calls++
	s.code, s.refresh = code, refresh
	return catalogue.BrowseCached[domain.ExternalRating]{Data: domain.ExternalRating{Source: "javlibrary", Max: 10}}, nil
}

func TestBrowseArchiveRoutesTransferBinaryAndRemainProtected(t *testing.T) {
	s := &archiveAPIStub{}
	router := NewRouter(Dependencies{Access: NewAccessGateService("", ""), BrowseExtension: s})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/extensions/browse/cache/export", nil))
	if w.Code != 200 || w.Body.String() != "PK-archive" || w.Header().Get("Content-Type") != "application/zip" || !strings.Contains(w.Header().Get("Content-Disposition"), ".zip") || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("export: %d %v %s", w.Code, w.Header(), w.Body)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("POST", "/api/extensions/browse/cache/import", strings.NewReader("PK-upload")))
	if w.Code != 200 || s.code != "PK-upload" || !strings.Contains(w.Body.String(), `"movies":3`) {
		t.Fatalf("import: %d %s", w.Code, w.Body)
	}
	for _, methodPath := range [][2]string{{"GET", "cache/export"}, {"POST", "cache/import"}, {"GET", "ratings/javlibrary?code=ABP-123"}} {
		protected := NewRouter(Dependencies{Access: NewAccessGateService("password", "secret"), BrowseExtension: s})
		calls := s.calls
		w := httptest.NewRecorder()
		protected.ServeHTTP(w, httptest.NewRequest(methodPath[0], "/api/extensions/browse/"+methodPath[1], nil))
		if w.Code != 401 || s.calls != calls {
			t.Fatalf("unprotected archive route: %v %d", methodPath, w.Code)
		}
	}
	req := httptest.NewRequest("POST", "/api/extensions/browse/cache/import", strings.NewReader("PK"))
	req.Header.Set("Origin", "https://evil.invalid")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 403 || s.calls != 2 {
		t.Fatalf("cross-origin upload accepted: %d", w.Code)
	}
}

func TestBrowseRatingCacheOnlyRoutesNormalizeCodesAndValidateFlags(t *testing.T) {
	for _, test := range []struct {
		path         string
		status       int
		code, source string
	}{
		{"ratings/javlibrary?code=abp-123&cached_only=1", 200, "ABP-123", "javlibrary-code"},
		{"ratings/javlibrary?code=ABP-123&refresh=1", 200, "ABP-123", ""},
		{"movies/film/ratings/javlibrary?cached_only=1", 200, "film", "javlibrary"},
		{"ratings/javlibrary?code=", 400, "", ""},
		{"ratings/javlibrary?code=ABP-123&cached_only=bad", 400, "", ""},
		{"movies/film/ratings/javdb?cached_only=bad", 400, "", ""},
	} {
		s := &archiveAPIStub{}
		router := NewRouter(Dependencies{Access: NewAccessGateService("", ""), BrowseExtension: s})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", "/api/extensions/browse/"+test.path, nil))
		if w.Code != test.status || s.code != test.code || s.source != test.source {
			t.Fatalf("%s: %d code=%q source=%q", test.path, w.Code, s.code, s.source)
		}
	}
}
