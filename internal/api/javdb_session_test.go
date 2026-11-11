package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type javdbSessionStub struct {
	CatalogueManager
	configured bool
}

func (s *javdbSessionStub) JavDBSessionConfigured() bool { return s.configured }

func (s *javdbSessionStub) LoginJavDB(context.Context, string, string) error {
	s.configured = true
	return nil
}

func (s *javdbSessionStub) LogoutJavDB(context.Context) error {
	s.configured = false
	return nil
}

func TestJavDBSessionRoutesRequireAppLoginAndExposeOnlyStatus(t *testing.T) {
	gate := NewAccessGateService("password", "test-signing-key")
	cookie, _, err := gate.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	service := &javdbSessionStub{}
	router := NewRouter(Dependencies{Access: gate, Catalogue: service, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	const path = "/api/settings/javdb/session"
	const credentials = `{"username":"account","password":"test password"}`
	unauthorized := httptest.NewRecorder()
	router.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, path, strings.NewReader(credentials)))
	if unauthorized.Code != http.StatusUnauthorized || service.configured {
		t.Fatal("unauthenticated requests must not install a session")
	}
	for _, tc := range []struct{ method, body, want string }{
		{http.MethodGet, "", `{"configured":false}`},
		{http.MethodPost, credentials, `{"configured":true}`},
		{http.MethodGet, "", `{"configured":true}`},
		{http.MethodDelete, "", `{"configured":false}`},
	} {
		request := httptest.NewRequest(tc.method, path, strings.NewReader(tc.body))
		request.AddCookie(&http.Cookie{Name: cookieAuthToken, Value: cookie})
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Body.String() != tc.want || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s response: status=%d body=%q", tc.method, response.Code, response.Body)
		}
	}
}
