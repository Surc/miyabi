package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	fhttp "github.com/bogdanfinn/fhttp"
	"github.com/bogdanfinn/websocket"
	"github.com/ppxb/miyabi/internal/api"
	"github.com/ppxb/miyabi/internal/catalogue"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/javlibrary"
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

func TestBrowserRelayServesRatingsForTheConnectingComputer(t *testing.T) {
	token := strings.Repeat("c", 64)
	path := filepath.Join(t.TempDir(), "browser.json")
	body, _ := json.Marshal(javlibrary.BrowserConfig{Enabled: true, ConnectBack: true, APIToken: token})
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MIYABI_JAVINIZER_CONFIG", "")
	t.Setenv("MIYABI_JAVLIBRARY_BROWSER_CONFIG", path)
	reader, err := newExternalRating()
	if err != nil {
		t.Fatal(err)
	}
	relay, ok := reader.(*javlibrary.BrowserRelay)
	if !ok {
		t.Fatal("expected browser relay")
	}
	extras := catalogue.NewMovieExtrasWithRating(&catalogue.Service{}, relay)
	defer extras.Close()
	browse, err := catalogue.NewBrowseExtension(t.TempDir(), nil, extras)
	if err != nil {
		t.Fatal(err)
	}
	const ratingPath = "/api/extensions/movie/javlibrary-rating?code=ABP-123"
	const browsePath = "/api/extensions/browse/ratings/javlibrary?code=FSDSS-437"
	const relayPath = "/api/extensions/movie/javlibrary-browser"
	router := api.NewRouter(api.Dependencies{Access: api.NewAccessGateService("", ""), MovieExtras: extras, JavLibraryRelay: relay, BrowseExtension: browse})
	server := httptest.NewServer(router)
	defer server.Close()
	request := func(path, remote string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = remote
		req.Header.Set("X-Forwarded-For", "127.0.0.1")
		result := httptest.NewRecorder()
		router.ServeHTTP(result, req)
		return result
	}
	if result := request(relayPath, "127.0.0.1:56789"); result.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated relay returned %d", result.Code)
	}
	if result := request(browsePath, "127.0.0.1:56789"); result.Code != http.StatusBadGateway || !strings.Contains(result.Body.String(), "尚未连接") {
		t.Fatalf("offline browser: %d %s", result.Code, result.Body)
	}

	// The API needs only the helper's token. Node's WebSocket client sends no Origin.
	address := "ws" + strings.TrimPrefix(server.URL, "http") + relayPath
	conn, _, err := websocket.DefaultDialer.DialContext(t.Context(), address, fhttp.Header{"Authorization": {"Bearer " + token}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if deadline, ok := t.Deadline(); ok {
		_ = conn.SetReadDeadline(deadline)
		_ = conn.SetWriteDeadline(deadline)
	}
	// A heartbeat round-trip confirms registration before sending rating requests.
	if err := conn.WriteJSON(struct{}{}); err != nil {
		t.Fatal(err)
	}
	var heartbeat struct{}
	if err := conn.ReadJSON(&heartbeat); err != nil {
		t.Fatal(err)
	}
	replies := make(chan error, 1)
	go func() {
		for _, code := range []string{"ABP-123", "FSDSS-437"} {
			var job struct {
				ID  uint64 `json:"id"`
				URL string `json:"url"`
			}
			if err := conn.ReadJSON(&job); err != nil {
				replies <- err
				return
			}
			target := "https://www.javlibrary.com/cn/javfixture.html"
			if err := conn.WriteJSON(map[string]any{
				"id": job.ID, "url": target, "status": 200, "redirects": []string{target},
				"html": `<div id="video_id"><span class="text">` + code + `</span></div><div id="video_review"><span class="score">(8.40)</span></div>`,
			}); err != nil {
				replies <- err
				return
			}
		}
		replies <- nil
	}()

	var direct domain.ExternalRating
	result := request(ratingPath, "[::ffff:127.0.0.1]:56789")
	if result.Code != http.StatusOK || json.Unmarshal(result.Body.Bytes(), &direct) != nil || !direct.Found || direct.Score == nil || *direct.Score != 8.4 {
		t.Fatalf("browser rating: %d %s", result.Code, result.Body)
	}
	if result = request(ratingPath, "192.0.2.4:56789"); result.Code != http.StatusBadGateway {
		t.Fatalf("rating leaked across computers or trusted a spoofed header: %d %s", result.Code, result.Body)
	}
	var cached catalogue.BrowseCached[domain.ExternalRating]
	result = request(browsePath, "127.0.0.1:56789")
	if result.Code != http.StatusOK || json.Unmarshal(result.Body.Bytes(), &cached) != nil || !cached.Data.Found || cached.Data.Score == nil || *cached.Data.Score != 8.4 {
		t.Fatalf("browse rating: %d %s", result.Code, result.Body)
	}
	if err := <-replies; err != nil {
		t.Fatal(err)
	}
}
