package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ppxb/miyabi/internal/domain"
)

type extrasStub struct {
	calls            int
	source, id, code string
	page             int
}

func (s *extrasStub) JavLibraryRating(_ context.Context, code string) (domain.ExternalRating, error) {
	s.calls++
	s.code = code
	score := 8.5
	return domain.ExternalRating{Source: "javlibrary", Max: 10, Found: true, Score: &score}, nil
}
func (s *extrasStub) MovieComments(_ context.Context, source, id, code string, page int) (domain.MovieComments, error) {
	s.calls++
	s.source, s.id, s.code, s.page = source, id, code, page
	return domain.MovieComments{Source: source, Page: page, Items: []domain.MovieComment{}}, nil
}

func TestExtrasRoutesValidateAndPreserveAuth(t *testing.T) {
	for _, test := range []struct {
		query  string
		status int
	}{
		{"javlibrary-rating?code=abp-123", 200},
		{"javlibrary-rating?code=%20", 400},
		{"comments?source=javdb&javdb_id=film&page=2", 200},
		{"comments?source=javlibrary&code=ABP-123", 200},
		{"comments?source=evil&code=ABP-123", 400},
		{"comments?source=javdb", 400},
		{"comments?source=javlibrary", 400},
		{"comments?source=javdb&javdb_id=film&page=1001", 400},
		{"comments?source=javdb&javdb_id=film&page=0", 400},
	} {
		t.Run(test.query, func(t *testing.T) {
			stub := &extrasStub{}
			router := NewRouter(Dependencies{Access: NewAccessGateService("", ""), MovieExtras: stub})
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/extensions/movie/"+test.query, nil))
			if w.Code != test.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			if test.status == 200 {
				if stub.calls != 1 || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
					t.Fatalf("calls=%d headers=%v", stub.calls, w.Header())
				}
			} else if stub.calls != 0 {
				t.Fatal("invalid input reached manager")
			}
		})
	}
	stub := &extrasStub{}
	router := NewRouter(Dependencies{Access: NewAccessGateService("password", "secret"), MovieExtras: stub})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/extensions/movie/javlibrary-rating?code=ABP-123", nil))
	if w.Code != 401 || stub.calls != 0 {
		t.Fatalf("auth status=%d calls=%d", w.Code, stub.calls)
	}
}
