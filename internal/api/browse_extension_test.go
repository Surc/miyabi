package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ppxb/miyabi/internal/catalogue"
	"github.com/ppxb/miyabi/internal/domain"
)

type browseAPIStub struct {
	BrowseExtensionManager
	calls   int
	refresh bool
	page    int
	source  string
}

func (s *browseAPIStub) BrowsePage(_ context.Context, q domain.BrowseOptions, refresh bool) (catalogue.BrowseCached[[]catalogue.Movie], error) {
	s.calls++
	s.page = q.Page
	s.refresh = refresh
	return catalogue.BrowseCached[[]catalogue.Movie]{Data: []catalogue.Movie{}}, nil
}
func (s *browseAPIStub) SearchPage(_ context.Context, _ string, q domain.SearchOptions, refresh bool) (catalogue.BrowseCached[[]catalogue.Movie], error) {
	s.calls++
	s.page = q.Page
	s.refresh = refresh
	return catalogue.BrowseCached[[]catalogue.Movie]{Data: []catalogue.Movie{}}, nil
}
func (s *browseAPIStub) Resources(_ context.Context, _ string, refresh bool) (catalogue.BrowseCached[catalogue.BrowseResources], error) {
	s.calls++
	s.refresh = refresh
	return catalogue.BrowseCached[catalogue.BrowseResources]{}, nil
}
func (s *browseAPIStub) Rating(_ context.Context, source, _ string, refresh bool) (catalogue.BrowseCached[domain.ExternalRating], error) {
	s.calls++
	s.source = source
	s.refresh = refresh
	return catalogue.BrowseCached[domain.ExternalRating]{}, nil
}
func (s *browseAPIStub) Image(context.Context, string) (domain.Media, error) {
	s.calls++
	return domain.Media{ContentType: "image/png", Body: []byte("image")}, nil
}
func TestBrowseExtensionRoutesPreserveValidationAndAuthentication(t *testing.T) {
	for _, test := range []struct {
		path   string
		status int
	}{
		{"movies?page=2&limit=20&sort=release&order=desc", 200},
		{"movies?page=0", 400}, {"movies?limit=101", 400}, {"movies?zone=wrong", 400},
		{"search?q=ABP-123&page=3", 200}, {"search?q=", 400},
		{"movies/film/resources?refresh=1", 200}, {"movies/film/resources?refresh=bad", 400},
		{"movies/film/ratings/javlibrary", 200}, {"movies/film/ratings/evil", 400},
		{"movies/film/image", 200},
	} {
		t.Run(test.path, func(t *testing.T) {
			s := &browseAPIStub{}
			router := NewRouter(Dependencies{Access: NewAccessGateService("", ""), BrowseExtension: s})
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest("GET", "/api/extensions/browse/"+test.path, nil))
			if w.Code != test.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			if test.status == 200 && (s.calls != 1 || !strings.Contains(w.Header().Get("Cache-Control"), "no-store")) {
				t.Fatalf("calls=%d headers=%v", s.calls, w.Header())
			}
			if test.status != 200 && s.calls != 0 {
				t.Fatal("invalid request reached service")
			}
			if strings.Contains(test.path, "refresh=1") && !s.refresh {
				t.Fatal("refresh flag lost")
			}
		})
	}
	s := &browseAPIStub{}
	router := NewRouter(Dependencies{Access: NewAccessGateService("password", "secret"), BrowseExtension: s})
	for _, path := range []string{"movies", "movies/film/resources", "movies/film/image", "settings", "cache"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", "/api/extensions/browse/"+path, nil))
		if w.Code != 401 || s.calls != 0 {
			t.Fatalf("auth %s %d", path, w.Code)
		}
	}
	public := NewRouter(Dependencies{Access: NewAccessGateService("", ""), BrowseExtension: s})
	req := httptest.NewRequest("DELETE", "/api/extensions/browse/cache?scope=all", nil)
	req.Header.Set("Origin", "https://evil.invalid")
	w := httptest.NewRecorder()
	public.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("same-origin=%d", w.Code)
	}
	w = httptest.NewRecorder()
	NewRouter(Dependencies{Access: NewAccessGateService("", "")}).ServeHTTP(w, httptest.NewRequest("GET", "/api/extensions/browse/settings", nil))
	if w.Code != 404 {
		t.Fatal("optional routes registered without a service")
	}
}
