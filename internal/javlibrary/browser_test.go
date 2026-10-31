package javlibrary

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func browserFixture(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := NewBrowserRatingReader(BrowserConfig{Enabled: true, ServerURL: server.URL, APIToken: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client
}

func sendBrowserPage(w http.ResponseWriter, address, body string, redirects []string) {
	w.Header().Set("X-Miyabi-Browser-Page", "1")
	json.NewEncoder(w).Encode(map[string]any{"url": address, "status": 200, "html": body, "redirects": redirects})
}

func TestBrowserRatingKeepsRealPageIdentityAndTenPointScale(t *testing.T) {
	requests := 0
	client := browserFixture(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != "POST" || r.URL.Path != "/v1/page" || r.Header.Get("Authorization") != "Bearer "+strings.Repeat("a", 64) {
			t.Error("browser connection credentials or endpoint not applied")
		}
		var request struct {
			URL       string `json:"url"`
			TimeoutMS int    `json:"timeout_ms"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil || request.TimeoutMS < 1 || request.TimeoutMS > 30000 {
			t.Error("invalid bounded browser request")
		}
		if strings.Contains(request.URL, "vl_searchbyid") {
			sendBrowserPage(w, request.URL, `<div class="videos"><a href="?v=film"><span class="id">ABP-123</span></a></div>`, nil)
		} else {
			sendBrowserPage(w, request.URL, filmHTML, nil)
		}
	})
	rating, err := client.Rating(t.Context(), " abp-123 ")
	if err != nil || !rating.Found || rating.Score == nil || *rating.Score != 8.25 || rating.Max != 10 || requests != 2 {
		t.Fatalf("rating=%+v err=%v requests=%d", rating, err, requests)
	}
}

func TestBrowserRatingPreservesSearchRedirectAndRejectsInvalidPages(t *testing.T) {
	for _, mode := range []string{"redirect", "foreign", "credentials", "wrong film", "challenge", "missing chain", "too many redirects", "missing contract"} {
		t.Run(mode, func(t *testing.T) {
			client := browserFixture(t, func(w http.ResponseWriter, r *http.Request) {
				address, body := site+"?v=film", filmHTML
				redirects := []string{address}
				switch mode {
				case "foreign":
					address = "https://elsewhere.invalid/cn/?v=film"
				case "credentials":
					address = "https://user:secret@www.javlibrary.com/cn/?v=film"
				case "wrong film":
					body = strings.ReplaceAll(body, "ABP-123", "ABP-1234")
				case "challenge":
					body = `<title>Just a moment...</title><form id="challenge-form"></form>`
				case "missing chain":
					redirects = nil
				case "too many redirects":
					redirects = []string{address, address, address}
				case "missing contract":
					json.NewEncoder(w).Encode(map[string]any{"url": address, "status": 200, "html": body, "redirects": redirects})
					return
				}
				sendBrowserPage(w, address, body, redirects)
			})
			rating, err := client.Rating(t.Context(), "ABP-123")
			if mode == "redirect" {
				if err != nil || rating.Score == nil {
					t.Fatalf("valid browser redirect failed: %+v %v", rating, err)
				}
			} else if err == nil || rating.Score != nil {
				t.Fatalf("unverified browser page accepted: %+v %v", rating, err)
			}
		})
	}
}

func TestBrowserVerificationRequiredIsRetryableAfterHumanConfirmation(t *testing.T) {
	requests := 0
	client := browserFixture(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte(`{"error":"verification_required"}`))
			return
		}
		address := site + "?v=film"
		sendBrowserPage(w, address, filmHTML, []string{address})
	})
	_, err := client.Rating(t.Context(), "ABP-123")
	if !errors.Is(err, ErrVerificationRequired) {
		t.Fatalf("verification requirement lost: %v", err)
	}
	rating, err := client.Rating(t.Context(), "ABP-123")
	if err != nil || rating.Score == nil || requests != 2 {
		t.Fatalf("retry after confirmation failed: %+v %v", rating, err)
	}
}

func TestBrowserCancellationAndSecretsStayBounded(t *testing.T) {
	started := make(chan struct{})
	client := browserFixture(t, func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := client.Rating(ctx, "ABP-123"); done <- err }()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), strings.Repeat("a", 64)) {
			t.Fatalf("cancellation or secret redaction failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("browser request ignored cancellation")
	}
}

func TestBrowserConfigIsExplicitAndDoesNotExposeInvalidSecrets(t *testing.T) {
	if config, err := LoadBrowserConfig(""); err != nil || config.Enabled {
		t.Fatalf("unexpected default: %+v %v", config, err)
	}
	path := filepath.Join(t.TempDir(), "browser.json")
	for _, body := range []string{`null`, `{"enabled":true,"server_url":"http://user:secret@host/","api_token":"private"}`, `{"enabled":false,"typo":true}`, `{} {}`} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadBrowserConfig(path); err == nil || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "secret") {
			t.Fatalf("invalid explicit config accepted or leaked: %v", err)
		}
	}
}
