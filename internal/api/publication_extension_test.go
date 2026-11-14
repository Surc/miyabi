package api

import (
	"github.com/ppxb/miyabi/internal/export"
	"net/http/httptest"
	"strings"
	"testing"
)

type publicationStatusStub struct{ calls int }

func (s *publicationStatusStub) PublicationStatus() export.PublicationStatus {
	s.calls++
	return export.PublicationStatus{Blocked: true, Pending: 1}
}
func TestPublicationStatusIsOptionalAuthenticatedAndReadOnly(t *testing.T) {
	stub := &publicationStatusStub{}
	path := "/api/extensions/publication/status"
	denied := httptest.NewRecorder()
	NewRouter(Dependencies{Access: NewAccessGateService("password", "key"), Publication: stub}).ServeHTTP(denied, httptest.NewRequest("GET", path, nil))
	if denied.Code != 401 || stub.calls != 0 {
		t.Fatal("unauthenticated status access")
	}
	absent := httptest.NewRecorder()
	NewRouter(Dependencies{Access: NewAccessGateService("", "")}).ServeHTTP(absent, httptest.NewRequest("GET", path, nil))
	if absent.Code != 404 {
		t.Fatal("optional status route mounted without service")
	}
	response := httptest.NewRecorder()
	NewRouter(Dependencies{Access: NewAccessGateService("", ""), Publication: stub}).ServeHTTP(response, httptest.NewRequest("GET", path, nil))
	if response.Code != 200 || stub.calls != 1 || !strings.Contains(response.Body.String(), `"blocked":true`) || !strings.Contains(response.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("unexpected status: %s", response.Body)
	}
}
