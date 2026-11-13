package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/javdb"
)

type javdbSessionStub struct {
	CatalogueManager
	status   javdb.SessionStatus
	loginErr error
}

func (s *javdbSessionStub) JavDBSessionStatus(context.Context) javdb.SessionStatus { return s.status }

func (s *javdbSessionStub) LoginJavDB(context.Context, string, string) error {
	if s.loginErr != nil {
		return s.loginErr
	}
	s.status = javdb.SessionSignedIn
	return nil
}

func (s *javdbSessionStub) LogoutJavDB(context.Context) error {
	s.status = javdb.SessionSignedOut
	return nil
}

func TestJavDBSessionRoutesRequireAppLoginAndExposeOnlyStatus(t *testing.T) {
	gate := NewAccessGateService("password", "test-signing-key")
	cookie, _, err := gate.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	service := &javdbSessionStub{status: javdb.SessionSignedOut}
	router := NewRouter(Dependencies{Access: gate, Catalogue: service, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	const path = "/api/settings/javdb/session"
	const credentials = `{"username":"account","password":"test password"}`
	unauthorized := httptest.NewRecorder()
	router.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, path, strings.NewReader(credentials)))
	if unauthorized.Code != http.StatusUnauthorized || service.status != javdb.SessionSignedOut {
		t.Fatal("unauthenticated requests must not install a session")
	}
	check := func(method, body string, status int, want string) {
		t.Helper()
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.AddCookie(&http.Cookie{Name: cookieAuthToken, Value: cookie})
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != status || response.Body.String() != want || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s response: status=%d body=%q", method, response.Code, response.Body)
		}
	}
	check(http.MethodGet, "", http.StatusOK, `{"configured":false,"status":"signed_out"}`)
	check(http.MethodPost, credentials, http.StatusOK, `{"configured":true,"status":"signed_in"}`)
	check(http.MethodGet, "", http.StatusOK, `{"configured":true,"status":"signed_in"}`)
	service.loginErr = domain.E(domain.KindUpstream, "JavDB 登录失败", nil)
	check(http.MethodPost, credentials, http.StatusBadGateway, `{"error":"JavDB 登录失败"}`)
	check(http.MethodGet, "", http.StatusOK, `{"configured":true,"status":"signed_in"}`)
	service.status = javdb.SessionExpired
	check(http.MethodGet, "", http.StatusOK, `{"configured":true,"status":"expired"}`)
	service.status = javdb.SessionUnverified
	check(http.MethodGet, "", http.StatusOK, `{"configured":true,"status":"unverified"}`)
	check(http.MethodDelete, "", http.StatusOK, `{"configured":false,"status":"signed_out"}`)
}
