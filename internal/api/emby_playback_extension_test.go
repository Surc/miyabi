package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ppxb/miyabi/internal/emby"
)

type playbackAPIStub struct {
	calls int
	code  string
}

func (s *playbackAPIStub) PlaybackTarget(_ context.Context, code string) (emby.PlaybackTarget, error) {
	s.calls++
	s.code = code
	return emby.PlaybackTarget{Status: "ready", URL: "emby://play/server/item"}, nil
}

func TestPlaybackExtensionValidatesAndKeepsAuthentication(t *testing.T) {
	stub := &playbackAPIStub{}
	router := NewRouter(Dependencies{Access: NewAccessGateService("", ""), EmbyPlayback: stub})
	for _, query := range []struct {
		value  string
		status int
	}{{"?code=ABC-123", 200}, {"", 400}, {"?code=" + strings.Repeat("A", 201), 400}} {
		before := stub.calls
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", "/api/extensions/emby/playback"+query.value, nil))
		if w.Code != query.status {
			t.Fatalf("status=%d body=%s", w.Code, w.Body)
		}
		if query.status == 200 && (stub.code != "ABC-123" || !strings.Contains(w.Body.String(), "emby://play/") || !strings.Contains(w.Header().Get("Cache-Control"), "no-store")) {
			t.Fatalf("response=%s headers=%v", w.Body, w.Header())
		}
		if query.status != 200 && before != stub.calls {
			t.Fatal("invalid request reached Emby")
		}
	}
	w := httptest.NewRecorder()
	NewRouter(Dependencies{Access: NewAccessGateService("password", "secret"), EmbyPlayback: stub}).ServeHTTP(w, httptest.NewRequest("GET", "/api/extensions/emby/playback?code=ABC-123", nil))
	if w.Code != 401 {
		t.Fatalf("auth=%d", w.Code)
	}
	w = httptest.NewRecorder()
	NewRouter(Dependencies{Access: NewAccessGateService("", "")}).ServeHTTP(w, httptest.NewRequest("GET", "/api/extensions/emby/playback?code=ABC-123", nil))
	if w.Code != 404 {
		t.Fatalf("optional capability=%d", w.Code)
	}
}
