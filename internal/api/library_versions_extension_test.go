package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/library"
)

type libraryVersionsStub struct {
	LibraryManager
	calls int
	err   error
}

func (s *libraryVersionsStub) ResourceVersions(_ context.Context, ids []int) ([]library.ResourceVersion, error) {
	s.calls++
	return []library.ResourceVersion{{ID: ids[0], Available: true, Labels: []string{"中文破解"}}}, s.err
}

func TestLibraryVersionsOptionalAuthenticatedAndBounded(t *testing.T) {
	const path = "/api/extensions/library/resource-versions"
	request := func(router interface {
		ServeHTTP(http.ResponseWriter, *http.Request)
	}, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(body)))
		return w
	}
	old := NewRouter(Dependencies{Access: NewAccessGateService("", ""), Library: &libraryPageStub{}})
	if w := request(old, `{"ids":[1]}`); w.Code != 404 {
		t.Fatalf("old interface changed: %d", w.Code)
	}
	stub := &libraryVersionsStub{}
	protected := NewRouter(Dependencies{Access: NewAccessGateService("password", "key"), Library: stub})
	if w := request(protected, `{"ids":[1]}`); w.Code != 401 || stub.calls != 0 {
		t.Fatal("authentication bypassed")
	}
	router := NewRouter(Dependencies{Access: NewAccessGateService("", ""), Library: stub})
	ids := make([]int, 101)
	for i := range ids {
		ids[i] = i + 1
	}
	tooMany, _ := json.Marshal(map[string][]int{"ids": ids})
	for _, body := range []string{`{}`, `{"ids":[]}`, `{"ids":[0]}`, `{"ids":[-1]}`, `{"ids":["1"]}`, string(tooMany), `{"ids":[1],"padding":"` + strings.Repeat("a", 8<<10) + `"}`} {
		if w := request(router, body); w.Code != 400 || stub.calls != 0 {
			t.Fatalf("invalid batch reached service: %d %s", w.Code, w.Body)
		}
	}
	w := request(router, `{"ids":[1,2]}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "中文破解") || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("response: %d %s", w.Code, w.Body)
	}
	stub.err = domain.E(domain.KindConflict, "source changed", nil)
	if w := request(router, `{"ids":[1]}`); w.Code != 409 {
		t.Fatalf("service error lost: %d", w.Code)
	}
}
