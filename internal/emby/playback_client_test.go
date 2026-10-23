package emby

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ppxb/miyabi/internal/domain"
)

func TestPlaybackValidatesBaseURLBeforeSendingCredentials(t *testing.T) {
	for _, raw := range []string{
		"https://emby.invalid/emby?key=private", "https://emby.invalid/?", "https://emby.invalid/#",
		"https://emby.invalid/#fragment", "https://user:private@emby.invalid/emby",
		"file:///emby", "//emby.invalid", "https:///emby", "https://emby.invalid:bad",
	} {
		t.Run(raw, func(t *testing.T) {
			s, _ := playbackTestService(t, func(http.ResponseWriter, *http.Request) { t.Error("invalid base URL sent a request") })
			s.cfg.ServerURL = raw
			target, err := s.PlaybackTarget(t.Context(), "ABC-123")
			if target.URL != "" || !domain.IsKind(err, domain.KindInvalid) || strings.Contains(err.Error(), "private") {
				t.Fatalf("target=%+v err=%v", target, err)
			}
		})
	}
}

func TestPlaybackQueriesRootDriveAndUNCPathsThroughAPIPrefix(t *testing.T) {
	for _, tc := range []struct{ root, query, returned string }{
		{"/", "/ABC/ABC-123/ABC-123.strm", "/ABC/ABC-123/ABC-123.strm"},
		{`D:\Media\`, `D:\Media\ABC\ABC-123\ABC-123.strm`, "d:/Media/ABC/ABC-123/ABC-123.strm"},
		{"//nas/share/", `\\nas\share\ABC\ABC-123\ABC-123.strm`, "//nas/share/ABC/ABC-123/ABC-123.strm"},
	} {
		t.Run(tc.root, func(t *testing.T) {
			s, _ := playbackTestService(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Emby-Token") != "secret-key" || strings.Contains(r.URL.String(), "secret-key") {
					t.Error("API key was not confined to the request header")
				}
				switch r.URL.Path {
				case "/emby/Items":
					if r.URL.Query().Get("Path") != tc.query {
						t.Errorf("query=%q want=%q", r.URL.Query().Get("Path"), tc.query)
					}
					json.NewEncoder(w).Encode(map[string]any{"Items": []any{map[string]any{"Id": "item", "Path": tc.returned}}})
				case "/emby/System/Info":
					w.Write([]byte(`{"Id":"server"}`))
				default:
					t.Errorf("lost API prefix: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			})
			s.cfg.ServerURL += "/"
			s.cfg.MediaPath = tc.root
			target, err := s.PlaybackTarget(t.Context(), "ABC-123")
			if err != nil || target.URL != "emby://play/server/item" {
				t.Fatalf("target=%+v err=%v", target, err)
			}
		})
	}
}

func TestPlaybackResponseLimitsAndInvalidJSONNeverCreateLaunchLinks(t *testing.T) {
	items := `{"Items":[{"Id":"item","Path":"/media/ABC/ABC-123/ABC-123.strm"}]}`
	info := `{"Id":"server"}`
	for _, tc := range []struct {
		name, endpoint, body string
		valid                bool
	}{
		{"items at limit", "/emby/Items", items + strings.Repeat(" ", playbackResponseLimit-len(items)), true},
		{"items over limit", "/emby/Items", items + strings.Repeat(" ", playbackResponseLimit+1-len(items)), false},
		{"info over limit", "/emby/System/Info", info + strings.Repeat(" ", playbackResponseLimit+1-len(info)), false},
		{"second object", "/emby/Items", items + `{}`, false},
		{"trailing garbage", "/emby/System/Info", info + "private", false},
		{"null items", "/emby/Items", `{"Items":null}`, false},
		{"null info", "/emby/System/Info", `null`, false},
		{"wrong items type", "/emby/Items", `{"Items":"private"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := playbackTestService(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == tc.endpoint:
					w.Write([]byte(tc.body))
				case r.URL.Path == "/emby/Items":
					w.Write([]byte(items))
				default:
					w.Write([]byte(info))
				}
			})
			target, err := s.PlaybackTarget(t.Context(), "ABC-123")
			if tc.valid {
				if err != nil || target.URL != "emby://play/server/item" {
					t.Fatalf("valid boundary rejected: target=%+v err=%v", target, err)
				}
			} else if target.URL != "" || target.Status != "" || !domain.IsKind(err, domain.KindUpstream) {
				t.Fatalf("target=%+v err=%v", target, err)
			}
		})
	}
}

func TestPlaybackHTTPFailuresNeverEchoUpstreamBody(t *testing.T) {
	for _, status := range []int{301, 401, 403, 404, 429, 500, 503} {
		s, _ := playbackTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			w.Write([]byte("echoed secret-key and private STRM token"))
		})
		target, err := s.PlaybackTarget(t.Context(), "ABC-123")
		kind := domain.KindUpstream
		if status == 401 || status == 403 {
			kind = domain.KindUnauthorized
		}
		if target.URL != "" || !domain.IsKind(err, kind) || strings.Contains(err.Error(), "secret-key") || strings.Contains(err.Error(), "private") {
			t.Fatalf("status=%d target=%+v err=%v", status, target, err)
		}
	}
}

func TestPlaybackSameOriginRedirectPreservesHeaderAndLimitsLoops(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("X-Emby-Token") != "secret-key" || r.URL.RawQuery != "" {
			t.Error("redirect changed authentication delivery")
		}
		if r.URL.Path != "/ok" {
			destination := "/ok"
			if r.URL.Path == "/loop" {
				destination = "/loop"
			}
			http.Redirect(w, r, destination, http.StatusTemporaryRedirect)
			return
		}
		w.Write([]byte(`{"Id":"server"}`))
	}))
	defer server.Close()
	client, err := newPlaybackClient(Config{ServerURL: server.URL, APIKey: "secret-key"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	var info struct {
		ID string `json:"Id"`
	}
	if err := client.get(t.Context(), "redirect", nil, &info); err != nil || info.ID != "server" || calls.Load() != 2 {
		t.Fatalf("info=%+v calls=%d err=%v", info, calls.Load(), err)
	}
	calls.Store(0)
	if err := client.get(t.Context(), "loop", nil, &info); !domain.IsKind(err, domain.KindUpstream) || calls.Load() != 3 {
		t.Fatalf("redirect loop: calls=%d err=%v", calls.Load(), err)
	}
}

func TestPlaybackCancellationDuringResponseDoesNotBecomeMissing(t *testing.T) {
	started := make(chan struct{})
	s, _ := playbackTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"Items":`))
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		target, err := s.PlaybackTarget(ctx, "ABC-123")
		if target.URL != "" || target.Status != "" {
			t.Errorf("canceled target=%+v", target)
		}
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}
