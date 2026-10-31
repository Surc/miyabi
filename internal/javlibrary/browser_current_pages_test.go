package javlibrary

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestBrowserRatingParsesCurrentPagesAfterCanonicalRedirect(t *testing.T) {
	for _, test := range []struct {
		code, id string
		score    float64
	}{
		{"FSDSS-437", "javme4ivte", 8.4},
		{"IPZZ-982", "javme33a5a", 0},
	} {
		t.Run(test.code, func(t *testing.T) {
			body, err := os.ReadFile("testdata/" + strings.ToLower(test.code) + ".html")
			if err != nil {
				t.Fatal(err)
			}
			token := strings.Repeat("b", 64)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/v1/page" || r.Header.Get("Authorization") != "Bearer "+token {
					t.Error("unexpected browser helper request")
				}
				var request struct {
					URL string `json:"url"`
				}
				if json.NewDecoder(r.Body).Decode(&request) != nil || request.URL != site+"vl_searchbyid.php?keyword="+test.code {
					t.Errorf("unexpected page request: %s", request.URL)
				}
				location := site + test.id + ".html"
				w.Header().Set("X-Miyabi-Browser-Page", "1")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"url": location, "status": 200, "html": string(body), "redirects": []string{location},
				})
			}))
			t.Cleanup(server.Close)
			client, err := NewBrowserRatingReader(BrowserConfig{Enabled: true, ServerURL: server.URL, APIToken: token})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(client.Close)
			rating, err := client.Rating(t.Context(), test.code)
			if err != nil || !rating.Found || rating.Max != 10 || rating.Source != "javlibrary" {
				t.Fatalf("rating=%+v err=%v", rating, err)
			}
			if test.score == 0 {
				if rating.Score != nil {
					t.Fatalf("unrated film received a score: %v", *rating.Score)
				}
			} else if rating.Score == nil || *rating.Score != test.score {
				t.Fatalf("wrong native score: %+v", rating)
			}
		})
	}
}

func TestBrowserAllowsCanonicalFilmPagesWithoutBroadeningTargets(t *testing.T) {
	for _, test := range []struct {
		address string
		allowed bool
	}{
		{"https://www.javlibrary.com/cn/javme4ivte.html", true},
		{"https://www.javlibrary.com/cn/javme33a5a.html", true},
		{"https://www.javlibrary.com/cn/javme4ivte.html?v=javme4ivte", true},
		{"https://www.javlibrary.com/cn/javme4ivte.html?v=other", false},
		{"https://www.javlibrary.com/cn/javme4ivte.html?v=javme4ivte&v=javme4ivte", false},
		{"https://www.javlibrary.com/cn/javme4ivte.html?unused=value", false},
		{"https://www.javlibrary.com/cn/javme4ivte.html?unused=%zz", false},
		{"https://www.javlibrary.com/ja/javme4ivte.html", false},
		{"https://www.javlibrary.com/cn/redirect.html", false},
		{"https://www.javlibrary.com/cn/javme4ivte.html/extra", false},
		{"https://evil.example/cn/javme4ivte.html", false},
		{"http://www.javlibrary.com/cn/javme4ivte.html", false},
	} {
		t.Run(test.address, func(t *testing.T) {
			address, err := url.Parse(test.address)
			if err != nil {
				t.Fatal(err)
			}
			if browserPageURL(address) != test.allowed {
				t.Fatalf("wrong target decision for %s", test.address)
			}
		})
	}
}
