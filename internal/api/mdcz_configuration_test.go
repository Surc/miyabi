package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ppxb/miyabi/internal/mdcz"
)

type mdczConfigurationStub struct{ mdczAPIStub }

func (s *mdczConfigurationStub) Configuration(context.Context) (mdcz.Configuration, error) {
	s.calls++
	return mdcz.Configuration{}, nil
}
func (s *mdczConfigurationStub) UpdateConfiguration(context.Context, mdcz.ConfigurationUpdate) (mdcz.Configuration, error) {
	s.calls++
	return mdcz.Configuration{}, nil
}

func TestMDCzConfigurationCapabilityIsOptionalAuthenticatedAndNotCached(t *testing.T) {
	const path = "/api/extensions/mdcz/configuration"
	old := &mdczAPIStub{}
	w := httptest.NewRecorder()
	NewRouter(Dependencies{Access: NewAccessGateService("", ""), MDCz: old}).ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	if w.Code != 404 {
		t.Fatalf("old manager required a new method: %d", w.Code)
	}
	stub := &mdczConfigurationStub{}
	for _, method := range []string{"GET", "PUT"} {
		w = httptest.NewRecorder()
		NewRouter(Dependencies{Access: NewAccessGateService("password", "key"), MDCz: stub}).ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(`{}`)))
		if w.Code != 401 || stub.calls != 0 {
			t.Fatal("configuration route bypassed authentication")
		}
	}
	router := NewRouter(Dependencies{Access: NewAccessGateService("", ""), MDCz: stub})
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	if w.Code != 200 || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("cacheable configuration: %d", w.Code)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("PUT", path, strings.NewReader(`{"configuration_version":1,"configuration_id":"old","values":{"llm_key":"`+strings.Repeat("a", 129<<10)+`"}}`)))
	if w.Code != 400 || stub.calls != 1 {
		t.Fatalf("oversized configuration reached service: %d", w.Code)
	}
}
