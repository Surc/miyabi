package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestExternalRatingFactoryReturnsNilWhenDisabled(t *testing.T) {
	for _, body := range []string{"", `{"enabled":false}`} {
		t.Run(map[bool]string{true: "unset", false: "disabled"}[body == ""], func(t *testing.T) {
			path := ""
			if body != "" {
				path = filepath.Join(t.TempDir(), "rating.json")
				if err := os.WriteFile(path, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("MIYABI_JAVINIZER_CONFIG", path)
			reader, err := newExternalRating()
			if err != nil || reader != nil {
				t.Fatalf("disabled factory must return a nil interface: %v %v", reader, err)
			}
		})
	}
}

func TestExternalRatingFactoryRejectsExplicitInvalidConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rating.json")
	t.Setenv("MIYABI_JAVINIZER_CONFIG", path)
	if reader, err := newExternalRating(); err == nil || reader != nil {
		t.Fatalf("missing explicit config silently fell back: %v %v", reader, err)
	}
	for _, body := range []string{`null`, `{"enabled":true}`, `{"enabled":true,"api_tokne":"private"}`} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if reader, err := newExternalRating(); err == nil || reader != nil {
			t.Fatalf("invalid config silently fell back: %v %v", reader, err)
		}
	}
}

func TestExternalRatingFactoryConnectsConfiguredReader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/scrape" || r.Header.Get("Authorization") != "Bearer jv_factory_test" {
			t.Error("factory did not apply configured endpoint and credentials")
		}
		var request struct {
			ID       string   `json:"id"`
			Force    bool     `json:"force"`
			Scrapers []string `json:"selected_scrapers"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.ID != "ABP-123" || !request.Force || len(request.Scrapers) != 1 || request.Scrapers[0] != "javlibrary" {
			t.Errorf("unexpected request: %+v", request)
		}
		w.Header().Set("X-Miyabi-JavLibrary-Identity", "page-v1")
		_, _ = io.WriteString(w, `{"cached":false,"sources_used":1,"movie":{"code":"ABP-123","id":"ABP-123","source_name":"javlibrary","rating_score":8.4}}`)
	}))
	defer server.Close()
	config := map[string]any{"enabled": true, "server_url": server.URL, "api_token": "jv_factory_test"}
	body, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "rating.json")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MIYABI_JAVINIZER_CONFIG", path)
	reader, err := newExternalRating()
	if err != nil || reader == nil {
		t.Fatalf("configured factory: %v %v", reader, err)
	}
	defer reader.Close()
	rating, err := reader.Rating(t.Context(), " abp-123 ")
	if err != nil || !rating.Found || rating.Max != 10 || rating.Source != "javlibrary" || rating.Score == nil || *rating.Score != 8.4 {
		t.Fatalf("configured reader result: %+v %v", rating, err)
	}
}
