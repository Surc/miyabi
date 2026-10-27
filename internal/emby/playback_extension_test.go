package emby

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/export"
	"github.com/ppxb/miyabi/internal/nfo"
)

func playbackTestService(t *testing.T, handler http.HandlerFunc) (*Service, string) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	root := t.TempDir()
	dir := export.EmbyMovieDir(root, "ABC-123")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ABC-123.strm"), []byte("https://miyabi.invalid/api/strm/play/file?token=private"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &Service{cfg: Config{Enabled: true, ServerURL: server.URL + "/emby", APIKey: "secret-key", LocalDir: root, MediaPath: "/media"}, client: newEmbyClient()}, dir
}

func TestPlaybackResolvesExactExportPathAndUsesDirectPlayIntent(t *testing.T) {
	var paths []string
	s, _ := playbackTestService(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Method != "GET" || r.Header.Get("X-Emby-Token") != "secret-key" || strings.Contains(r.URL.RawQuery, "secret-key") {
			t.Errorf("invalid request %s %s", r.Method, r.URL)
		}
		switch r.URL.Path {
		case "/emby/Items":
			if r.URL.Query().Get("Path") != "/media/ABC/ABC-123/ABC-123.strm" || r.URL.Query().Get("SearchTerm") != "" || r.URL.Query().Get("Fields") != "Path" {
				t.Errorf("not an exact path lookup: %s", r.URL)
			}
			w.Write([]byte(`{"Items":[{"Id":"movie-12","Path":"/media/ABC/ABC-123/ABC-123.strm"}]}`))
		case "/emby/System/Info":
			w.Write([]byte(`{"Id":"server-1"}`))
		default:
			t.Errorf("unexpected endpoint %s", r.URL)
		}
	})
	target, err := s.PlaybackTarget(t.Context(), "ABC-123")
	if err != nil || target.Status != "ready" || target.URL != "emby://play/server-1/movie-12" {
		t.Fatalf("target=%+v err=%v", target, err)
	}
	if target.WebURL != s.cfg.ServerURL+"/web/index.html#!/item?id=movie-12&serverId=server-1" {
		t.Fatalf("web target=%q", target.WebURL)
	}
	encoded, _ := json.Marshal(target)
	if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "private") || strings.Contains(string(encoded), "api_key") {
		t.Fatalf("credentials or stream leaked: %s", encoded)
	}
	if len(paths) != 2 {
		t.Fatalf("requests=%v", paths)
	}
}

func TestPlaybackWebTargetPreservesServerBasePath(t *testing.T) {
	for _, prefix := range []string{"", "/emby", "/proxy/emby/"} {
		t.Run(prefix, func(t *testing.T) {
			s, _ := playbackTestService(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/System/Info") {
					w.Write([]byte(`{"Id":"server-1"}`))
					return
				}
				w.Write([]byte(`{"Items":[{"Id":"movie-12","Path":"/media/ABC/ABC-123/ABC-123.strm"}]}`))
			})
			s.cfg.ServerURL = strings.TrimSuffix(s.cfg.ServerURL, "/emby") + prefix
			target, err := s.PlaybackTarget(t.Context(), "ABC-123")
			want := strings.TrimRight(s.cfg.ServerURL, "/") + "/web/index.html#!/item?id=movie-12&serverId=server-1"
			if err != nil || target.WebURL != want {
				t.Fatalf("web target=%q want=%q err=%v", target.WebURL, want, err)
			}
		})
	}
}

func TestPlaybackDoesNotGuessFromMismatchedOrAmbiguousItems(t *testing.T) {
	for _, scenario := range []struct {
		name, body string
		kind       domain.Kind
	}{
		{"other film", `{"Items":[{"Id":"1","Path":"/other/ABC-123.strm"}]}`, domain.KindUpstream},
		{"folder", `{"Items":[{"Id":"1","Path":"/media/ABC/ABC-123/ABC-123.strm","IsFolder":true}]}`, domain.KindUpstream},
		{"missing path", `{"Items":[{"Id":"1"}]}`, domain.KindUpstream},
		{"unsafe identifier", `{"Items":[{"Id":"1?token=secret","Path":"/media/ABC/ABC-123/ABC-123.strm"}]}`, domain.KindUpstream},
		{"ambiguous", `{"Items":[{"Id":"1","Path":"/media/ABC/ABC-123/ABC-123.strm"},{"Id":"2","Path":"/media/ABC/ABC-123/ABC-123.strm"}]}`, domain.KindConflict},
		{"missing items", `{}`, domain.KindUpstream},
		{"malformed", `invalid`, domain.KindUpstream},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			s, _ := playbackTestService(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/emby/Items" {
					t.Errorf("unconfirmed item used: %s", r.URL)
				}
				w.Write([]byte(scenario.body))
			})
			target, err := s.PlaybackTarget(t.Context(), "ABC-123")
			if target.URL != "" || target.WebURL != "" || !domain.IsKind(err, scenario.kind) {
				t.Fatalf("target=%+v err=%v", target, err)
			}
		})
	}
}

func TestPlaybackUnavailableAndCancellationRemainDistinct(t *testing.T) {
	calls := 0
	s, _ := playbackTestService(t, func(w http.ResponseWriter, _ *http.Request) { calls++; w.Write([]byte(`{"Items":[]}`)) })
	target, err := s.PlaybackTarget(t.Context(), "ABC-123")
	if err != nil || target.Status != "not_found" || calls != 1 {
		t.Fatalf("target=%+v err=%v calls=%d", target, err, calls)
	}
	target, err = s.PlaybackTarget(t.Context(), "DEF-123")
	if err != nil || target.Status != "not_exported" || calls != 1 {
		t.Fatalf("unexported target=%+v err=%v", target, err)
	}
	s.cfg.Enabled = false
	target, err = s.PlaybackTarget(t.Context(), "ABC-123")
	if err != nil || target.Status != "disabled" || calls != 1 {
		t.Fatalf("disabled target=%+v err=%v", target, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.PlaybackTarget(ctx, "ABC-123"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	for _, code := range []string{" ", strings.Repeat("A", 201), "ABC\n123"} {
		if _, err := s.PlaybackTarget(t.Context(), code); !domain.IsKind(err, domain.KindInvalid) {
			t.Fatalf("invalid code accepted: %q %v", code, err)
		}
	}
}

func TestPlaybackServerFailuresDoNotBecomeNotFoundOrLaunchLinks(t *testing.T) {
	for _, scenario := range []struct {
		name, endpoint, body string
		status               int
		kind                 domain.Kind
	}{
		{"missing API route", "/emby/Items", "not found", 404, domain.KindUpstream},
		{"invalid API key", "/emby/Items", "denied", 401, domain.KindUnauthorized},
		{"invalid server ID", "/emby/System/Info", `{"Id":"server/other"}`, 200, domain.KindUpstream},
		{"server info failed", "/emby/System/Info", "unavailable", 503, domain.KindUpstream},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			s, _ := playbackTestService(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == scenario.endpoint {
					w.WriteHeader(scenario.status)
					w.Write([]byte(scenario.body))
					return
				}
				w.Write([]byte(`{"Items":[{"Id":"1","Path":"/media/ABC/ABC-123/ABC-123.strm"}]}`))
			})
			target, err := s.PlaybackTarget(t.Context(), "ABC-123")
			if target.Status != "" || target.URL != "" || target.WebURL != "" || !domain.IsKind(err, scenario.kind) {
				t.Fatalf("target=%+v err=%v", target, err)
			}
		})
	}
}

func TestPlaybackRejectsCrossOriginRedirectsAndDoesNotChangeTheSharedClient(t *testing.T) {
	forwarded := 0
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { forwarded++ }))
	defer other.Close()
	s, _ := playbackTestService(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, http.StatusFound) })
	_, err := s.PlaybackTarget(t.Context(), "ABC-123")
	if err == nil || forwarded != 0 || s.client.http.CheckRedirect != nil {
		t.Fatalf("err=%v forwarded=%d shared client changed=%v", err, forwarded, s.client.http.CheckRedirect != nil)
	}
}

func TestPlaybackFileSelectionUsesGeneratedNamesAndNumericPartOrder(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"ABC-123-cd10.strm", "ABC-123-cd2.strm", "ABC-123-cd0.strm", "ABC-123-extra.strm", "ABC-1234.strm", "ABC-123.nfo"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(dir, "ABC-123-cd2.strm"), filepath.Join(dir, "ABC-123.strm")); err != nil {
		t.Fatal(err)
	}
	file, err := firstPlaybackFile(dir, nfo.FileStem("ABC-123"))
	if err != nil || file != "ABC-123-cd2.strm" {
		t.Fatalf("file=%s err=%v", file, err)
	}
	if err := os.Remove(filepath.Join(dir, "ABC-123.strm")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ABC-123.strm"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	file, err = firstPlaybackFile(dir, "ABC-123")
	if err != nil || file != "ABC-123.strm" {
		t.Fatalf("file=%s err=%v", file, err)
	}
}
