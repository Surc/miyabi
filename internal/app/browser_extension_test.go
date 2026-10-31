package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrowserRatingFactoryUsesConfiguredSessionChannel(t *testing.T) {
	t.Setenv("MIYABI_JAVINIZER_CONFIG", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/page" || r.Header.Get("Authorization") != "Bearer "+strings.Repeat("c", 64) {
			t.Error("configured browser endpoint not used")
		}
		var request struct {
			URL string `json:"url"`
		}
		json.NewDecoder(r.Body).Decode(&request)
		w.Header().Set("X-Miyabi-Browser-Page", "1")
		json.NewEncoder(w).Encode(map[string]any{"url": request.URL, "status": 200, "html": `<div class="videos"></div>`, "redirects": []string{}})
	}))
	defer server.Close()
	body, _ := json.Marshal(map[string]any{"enabled": true, "server_url": server.URL, "api_token": strings.Repeat("c", 64)})
	path := filepath.Join(t.TempDir(), "browser.json")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MIYABI_JAVLIBRARY_BROWSER_CONFIG", path)
	reader, err := newExternalRating()
	if err != nil || reader == nil {
		t.Fatalf("browser factory: %v %v", reader, err)
	}
	defer reader.Close()
	rating, err := reader.Rating(t.Context(), "ABP-123")
	if err != nil || rating.Found || rating.Max != 10 {
		t.Fatalf("empty validated search: %+v %v", rating, err)
	}
}

func TestBrowserRatingFactoryRejectsExplicitMissingConfig(t *testing.T) {
	t.Setenv("MIYABI_JAVINIZER_CONFIG", "")
	t.Setenv("MIYABI_JAVLIBRARY_BROWSER_CONFIG", filepath.Join(t.TempDir(), "missing.json"))
	if reader, err := newExternalRating(); err == nil || reader != nil {
		t.Fatalf("invalid browser config silently fell back: %v %v", reader, err)
	}
}
