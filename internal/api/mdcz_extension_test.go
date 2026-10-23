package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ppxb/miyabi/internal/mdcz"
)

type mdczAPIStub struct{ calls int }

func (s *mdczAPIStub) ExtensionSettings() mdcz.Settings { return mdcz.Settings{HasAPIKey: true} }
func (s *mdczAPIStub) UpdateExtensionSettings(context.Context, mdcz.Update) error {
	s.calls++
	return nil
}
func (s *mdczAPIStub) Test(context.Context, mdcz.Update) (mdcz.Health, error) {
	s.calls++
	return mdcz.Health{Protocol: 1}, nil
}
func (s *mdczAPIStub) Synopsis(context.Context, string) (mdcz.Synopsis, error) {
	s.calls++
	return mdcz.Synopsis{Plot: "Fixture synopsis"}, nil
}

func TestMDCzRoutesAreOptionalAuthenticatedAndNotCached(t *testing.T) {
	stub := &mdczAPIStub{}
	for _, path := range []string{"/settings", "/synopsis?code=TEST-001"} {
		w := httptest.NewRecorder()
		NewRouter(Dependencies{Access: NewAccessGateService("password", "key"), MDCz: stub}).ServeHTTP(w, httptest.NewRequest("GET", "/api/extensions/mdcz"+path, nil))
		if w.Code != 401 || stub.calls != 0 {
			t.Fatalf("unauthenticated access: %d", w.Code)
		}
		w = httptest.NewRecorder()
		NewRouter(Dependencies{Access: NewAccessGateService("", "")}).ServeHTTP(w, httptest.NewRequest("GET", "/api/extensions/mdcz"+path, nil))
		if w.Code != 404 {
			t.Fatalf("optional route: %d", w.Code)
		}
	}
	router := NewRouter(Dependencies{Access: NewAccessGateService("", ""), MDCz: stub})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/extensions/mdcz/settings", nil))
	if w.Code != 200 || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") || strings.Contains(w.Body.String(), `"api_key"`) {
		t.Fatalf("settings=%s", w.Body)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/extensions/mdcz/synopsis", nil))
	if w.Code != 400 || stub.calls != 0 {
		t.Fatal("missing code reached service")
	}
}
