package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ppxb/miyabi/internal/catalogue"
	"github.com/ppxb/miyabi/internal/domain"
)

type movieActionStub struct {
	LibraryManager
	id      int
	code    string
	javdbID string
	err     error
}

func (s *movieActionStub) RescrapeMovie(_ context.Context, id int, code, javdbID string) (domain.TaskInfo, error) {
	s.id, s.code, s.javdbID = id, code, javdbID
	return domain.TaskInfo{ID: 7, Type: "scan", Status: "queued", MovieID: id, Code: code, Rebuild: true}, s.err
}

func TestMovieActionsValidateInputAndReturnQueuedWorkflow(t *testing.T) {
	for _, tc := range []struct {
		path, body string
		status     int
	}{
		{"42", `{}`, http.StatusAccepted},
		{"42", `{"code":"IPX-123"}`, http.StatusAccepted},
		{"42", `{"javdb_id":"g0dk4G"}`, http.StatusAccepted},
		{"42", `{"javdb_id":"../other"}`, http.StatusBadRequest},
		{"42", `{"code":"IPX-123","javdb_id":"g0dk4G"}`, http.StatusBadRequest},
		{"0", `{}`, http.StatusBadRequest},
		{"bad", `{}`, http.StatusBadRequest},
		{"42", `{"code":3}`, http.StatusBadRequest},
		{"42", `{"code":"` + strings.Repeat("A", 121) + `"}`, http.StatusBadRequest},
	} {
		stub := &movieActionStub{}
		router := NewRouter(Dependencies{Access: NewAccessGateService("", ""), Library: stub, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
		request := httptest.NewRequest(http.MethodPost, "/api/library/movies/"+tc.path+"/scrape", strings.NewReader(tc.body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != tc.status {
			t.Fatalf("%s %s: status=%d, %s", tc.path, tc.body, response.Code, response.Body)
		}
		if tc.status == http.StatusAccepted && (stub.id != 42 || !strings.Contains(response.Body.String(), `"movie_id":42`)) {
			t.Fatal("lost movie target")
		}
		if tc.body == `{"javdb_id":"g0dk4G"}` && stub.javdbID != "g0dk4G" {
			t.Fatal("lost selected JavDB identity")
		}
		if tc.status != http.StatusAccepted && stub.id != 0 {
			t.Fatal("invalid request reached service")
		}
	}
}

type sameNumberCatalogue struct{ CatalogueManager }

func (sameNumberCatalogue) Search(context.Context, string, domain.SearchOptions) ([]catalogue.Movie, error) {
	return []catalogue.Movie{
		{Movie: domain.Movie{ID: "first", Code: "ABF-027"}},
		{Movie: domain.Movie{ID: "second", Code: "ABF-27"}},
		{Movie: domain.Movie{ID: "different", Code: "ABF-027A"}},
	}, nil
}

func TestDiscoverSearchCanFilterSameNumberCandidates(t *testing.T) {
	router := NewRouter(Dependencies{Access: NewAccessGateService("", ""), Catalogue: sameNumberCatalogue{}})
	request := httptest.NewRequest(http.MethodGet, "/api/discover/search?q=ABF-027&same_code=1", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	var movies []catalogue.Movie
	if err := json.Unmarshal(response.Body.Bytes(), &movies); err != nil || response.Code != http.StatusOK ||
		len(movies) != 2 || movies[0].ID != "first" || movies[1].ID != "second" {
		t.Fatalf("wrong same-number candidates: HTTP %d, %s, %v", response.Code, response.Body, err)
	}
}
