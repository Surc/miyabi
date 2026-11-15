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
	const commentsPath = "/api/extensions/browse/comments?source=javlibrary&code=FSDSS-437&page=2"
	const relayPath = "/api/extensions/movie/javlibrary-browser"
	const openPath = relayPath + "/open"
	const confirmPath = relayPath + "/confirm"
	gate := api.NewAccessGateService("test-password", "")
	session, _, err := gate.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	cookie := &http.Cookie{Name: "miyabi_token", Value: session}
	router := api.NewRouter(api.Dependencies{Access: gate, MovieExtras: extras, JavLibraryRelay: relay, BrowseExtension: browse})
	server := httptest.NewServer(router)
	defer server.Close()
	request := func(method, path, remote string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, nil)
		req.RemoteAddr = remote
		req.Header.Set("X-Forwarded-For", "127.0.0.1")
		req.AddCookie(cookie)
		result := httptest.NewRecorder()
		router.ServeHTTP(result, req)
		return result
	}
	unauthorized := httptest.NewRecorder()
	router.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, openPath, nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("verification without a Miyabi session: %d", unauthorized.Code)
	}
	foreign := httptest.NewRequest(http.MethodPost, openPath, nil)
	foreign.AddCookie(cookie)
	foreign.Header.Set("Origin", "https://external.example")
	forbidden := httptest.NewRecorder()
	router.ServeHTTP(forbidden, foreign)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("cross-origin verification: %d", forbidden.Code)
	}
	if result := request(http.MethodGet, relayPath, "127.0.0.1:56789"); result.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated relay returned %d", result.Code)
	}
	if result := request(http.MethodGet, browsePath, "127.0.0.1:56789"); result.Code != http.StatusBadGateway || !strings.Contains(result.Body.String(), "尚未连接") {
		t.Fatalf("offline browser: %d %s", result.Code, result.Body)
	}
	if result := request(http.MethodPost, openPath, "127.0.0.1:56789"); result.Code != http.StatusBadGateway || !strings.Contains(result.Body.String(), "JAVLIBRARY_BROWSER_DISCONNECTED") {
		t.Fatalf("offline verification: %d %s", result.Code, result.Body)
	}

	// The API needs only the helper's token. Node's WebSocket client sends no Origin.
	address := "ws" + strings.TrimPrefix(server.URL, "http") + relayPath
	connect := func(control bool) *websocket.Conn {
		t.Helper()
		header := fhttp.Header{"Authorization": {"Bearer " + token}}
		if control {
			header.Set("X-Miyabi-Browser-Control", "1")
		}
		conn, _, err := websocket.DefaultDialer.DialContext(t.Context(), address, header)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		if deadline, ok := t.Deadline(); ok {
			_ = conn.SetReadDeadline(deadline)
			_ = conn.SetWriteDeadline(deadline)
		}
		// A heartbeat round-trip confirms registration before routing any action.
		if err := conn.WriteJSON(struct{}{}); err != nil {
			t.Fatal(err)
		}
		var heartbeat struct{}
		if err := conn.ReadJSON(&heartbeat); err != nil {
			t.Fatal(err)
		}
		return conn
	}
	legacy := connect(false)
	if result := request(http.MethodPost, openPath, "127.0.0.1:56789"); result.Code != http.StatusConflict || !strings.Contains(result.Body.String(), "更新") {
		t.Fatalf("legacy helper returned %d %s", result.Code, result.Body)
	}
	legacy.Close()
	conn := connect(true)
	defer conn.Close()
	if result := request(http.MethodPost, openPath, "192.0.2.4:56789"); result.Code != http.StatusBadGateway {
		t.Fatalf("verification on the wrong computer: %d %s", result.Code, result.Body)
	}
	replies := make(chan error, 1)
	go func() {
		for _, control := range []struct{ action, failure string }{{"open", ""}, {"confirm", "verification_required"}, {"confirm", ""}} {
			var job struct {
				ID     uint64 `json:"id"`
				Action string `json:"action"`
				URL    string `json:"url"`
			}
			if err := conn.ReadJSON(&job); err != nil {
				replies <- err
				return
			}
			if job.Action != control.action || job.URL != "" {
				t.Errorf("expected %s without a page URL, got %+v", control.action, job)
			}
			if err := conn.WriteJSON(map[string]any{"id": job.ID, "error": control.failure}); err != nil {
				replies <- err
				return
			}
		}
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
		var job struct {
			ID  uint64 `json:"id"`
			URL string `json:"url"`
		}
		if err := conn.ReadJSON(&job); err != nil {
			replies <- err
			return
		}
		if job.URL != "https://www.javlibrary.com/cn/videocomments.php?page=2&v=javfixture" {
			t.Errorf("unexpected comments request %q", job.URL)
		}
		replies <- conn.WriteJSON(map[string]any{
			"id": job.ID, "url": job.URL, "status": 200,
			"html": `<div id="video_comments"><table class="comment" id="comment2"><tr><td class="t">Second page</td></tr></table></div>`,
		})
	}()
	for _, step := range []struct {
		path   string
		status int
		body   string
	}{
		{openPath, http.StatusOK, `"ok":true`},
		{confirmPath, http.StatusConflict, "JAVLIBRARY_VERIFICATION_REQUIRED"},
		{confirmPath, http.StatusOK, `"ok":true`},
	} {
		if result := request(http.MethodPost, step.path, "[::ffff:127.0.0.1]:56789"); result.Code != step.status || !strings.Contains(result.Body.String(), step.body) {
			t.Fatalf("%s: %d %s", step.path, result.Code, result.Body)
		}
	}

	var direct domain.ExternalRating
	result := request(http.MethodGet, ratingPath, "[::ffff:127.0.0.1]:56789")
	if result.Code != http.StatusOK || json.Unmarshal(result.Body.Bytes(), &direct) != nil || !direct.Found || direct.Score == nil || *direct.Score != 8.4 {
		t.Fatalf("browser rating: %d %s", result.Code, result.Body)
	}
	if result = request(http.MethodGet, ratingPath, "192.0.2.4:56789"); result.Code != http.StatusBadGateway {
		t.Fatalf("rating leaked across computers or trusted a spoofed header: %d %s", result.Code, result.Body)
	}
	var cached catalogue.BrowseCached[domain.ExternalRating]
	result = request(http.MethodGet, browsePath, "127.0.0.1:56789")
	if result.Code != http.StatusOK || json.Unmarshal(result.Body.Bytes(), &cached) != nil || !cached.Data.Found || cached.Data.Score == nil || *cached.Data.Score != 8.4 {
		t.Fatalf("browse rating: %d %s", result.Code, result.Body)
	}
	for _, peer := range []string{"127.0.0.1:56789", "192.0.2.4:56789"} {
		var page catalogue.BrowseCached[domain.MovieComments]
		result = request(http.MethodGet, commentsPath, peer)
		if result.Code != http.StatusOK || json.Unmarshal(result.Body.Bytes(), &page) != nil || page.Data.Page != 2 || len(page.Data.Items) != 1 || page.Data.Items[0].Content != "Second page" {
			t.Fatalf("browser comments: %d %s", result.Code, result.Body)
		}
	}
	if err := <-replies; err != nil {
		t.Fatal(err)
	}
}
