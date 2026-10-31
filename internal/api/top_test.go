package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ppxb/miyabi/internal/top"
)

func TestTopRoutes(t *testing.T) {
	router := NewRouter(Dependencies{Access: NewAccessGateService("", "")})
	for _, test := range []struct {
		path   string
		status int
	}{
		{"", 200},
		{"/movies", 200},
		{"/movies?board=2022&page=6&limit=48", 200},
		{"/movies?board=unknown", 404},
		{"/movies?page=0", 400},
		{"/movies?page=1000001", 400},
		{"/movies?limit=101", 400},
		{"/rankings?code=ipx_811", 200},
		{"/rankings?code=UNKNOWN-99999999", 200},
		{"/rankings", 400},
		{"/rankings?code=%20", 400},
	} {
		t.Run(test.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/top"+test.path, nil))
			if response.Code != test.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body)
			}
		})
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/top/movies?board=2022&page=6", nil))
	var page top.Page
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Board.ID != "2022" || len(page.Items) != 10 || page.Items[0].Rank != 241 {
		t.Fatalf("bad page: %+v", page)
	}

	protected := NewRouter(Dependencies{Access: NewAccessGateService("password", "secret")})
	for _, path := range []string{"", "/movies", "/rankings?code=IPX-811"} {
		response := httptest.NewRecorder()
		protected.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/top"+path, nil))
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("unprotected route %s: %d", path, response.Code)
		}
	}
}

type topSettingsStub struct {
	config top.Config
	saves  int
}

func (service *topSettingsStub) Config(context.Context) (top.Config, error) {
	return service.config, nil
}

func (service *topSettingsStub) UpdateConfig(_ context.Context, config top.Config) (top.Config, error) {
	if err := config.Validate(); err != nil {
		return top.Config{}, err
	}
	service.config = config
	service.saves++
	return config, nil
}

func TestTopSettingsRoutesValidateAndPreserveAuth(t *testing.T) {
	service := &topSettingsStub{config: top.DefaultConfig()}
	router := NewRouter(Dependencies{Access: NewAccessGateService("", ""), TopSettings: service})
	send := func(method, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, "/api/settings/top", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	read := send(http.MethodGet, "")
	var config top.Config
	if read.Code != http.StatusOK || read.Header().Get("Cache-Control") != "no-store" || json.Unmarshal(read.Body.Bytes(), &config) != nil || config != top.DefaultConfig() {
		t.Fatalf("GET: %d %s", read.Code, read.Body)
	}
	saved := send(http.MethodPut, `{"show_navigation":false,"show_rankings":false,"default_board":"2022"}`)
	if saved.Code != http.StatusOK || saved.Header().Get("Cache-Control") != "no-store" || service.config.ShowNavigation || service.config.ShowRankings || service.config.DefaultBoard != "2022" || service.saves != 1 {
		t.Fatalf("PUT: %d %s", saved.Code, saved.Body)
	}
	want := service.config
	for _, body := range []string{`{`, `{}`, `{"default_board":"missing"}`, `{"show_rankings":"false","default_board":"all"}`} {
		response := send(http.MethodPut, body)
		if response.Code != http.StatusBadRequest || service.config != want || service.saves != 1 {
			t.Fatalf("invalid %s: %d %s", body, response.Code, response.Body)
		}
	}
	protected := NewRouter(Dependencies{Access: NewAccessGateService("password", "secret"), TopSettings: service})
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		response := httptest.NewRecorder()
		protected.ServeHTTP(response, httptest.NewRequest(method, "/api/settings/top", strings.NewReader(`{"default_board":"all"}`)))
		if response.Code != http.StatusUnauthorized || service.saves != 1 {
			t.Fatalf("unauthenticated %s: %d %s", method, response.Code, response.Body)
		}
	}
}
