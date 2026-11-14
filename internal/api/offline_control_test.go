package api

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/ppxb/miyabi/internal/domain"
)

type controlledOffline struct {
	OfflineManager
	cancelled []int
	switched  []int
	addedHash string
	ed2kID    string
	ed2kPage  int
}

func (m *controlledOffline) Add(_ context.Context, _, hash string) (domain.OfflineSubmission, error) {
	m.addedHash = hash
	return domain.OfflineSubmission{}, nil
}

func (m *controlledOffline) AddED2K(_ context.Context, _, id string, page int) (domain.OfflineSubmission, error) {
	m.ed2kID, m.ed2kPage = id, page
	return domain.OfflineSubmission{}, nil
}

func (m *controlledOffline) Cancel(_ context.Context, id int) (domain.OfflineSubmission, error) {
	m.cancelled = append(m.cancelled, id)
	return domain.OfflineSubmission{TaskID: id, Status: "cancelled"}, nil
}

func TestOfflineAddValidatesMagnetAndED2KSelectors(t *testing.T) {
	manager := &controlledOffline{}
	router := gin.New()
	router.Use(errorMiddleware(slog.Default()))
	router.POST("/movies/:id/offline", offlineAddHandler(manager))
	const hash = "1234567890abcdef1234567890abcdef12345678"
	const id = "ed2k:1234567890abcdef1234567890abcdef:6994439218"
	for _, tc := range []struct {
		body string
		hash string
		id   string
		page int
	}{
		{body: `{"hash":"` + hash + `"}`, hash: hash},
		{body: `{"ed2k":"` + id + `","page":2}`, id: id, page: 2},
		{body: `{"ed2k":"` + id + `"}`, id: id, page: 1},
		{body: `{"hash":"` + hash[:32] + `"}`},
		{body: `{"hash":"` + hash + `","ed2k":"` + id + `"}`},
		{body: `{"ed2k":"ed2k://|file|different|1234|abcdef|/"}`},
		{body: `{"ed2k":"` + id + `","page":1001}`},
		{body: `{}`},
	} {
		*manager = controlledOffline{}
		request := httptest.NewRequest(http.MethodPost, "/movies/1AArv/offline", strings.NewReader(tc.body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		status := http.StatusAccepted
		if tc.hash == "" && tc.id == "" {
			status = http.StatusBadRequest
		}
		if response.Code != status || manager.addedHash != tc.hash || manager.ed2kID != tc.id || manager.ed2kPage != tc.page {
			t.Errorf("%s: status=%d hash=%q id=%q page=%d", tc.body, response.Code, manager.addedHash, manager.ed2kID, manager.ed2kPage)
		}
	}
}
func (m *controlledOffline) TryNext(_ context.Context, id int) (domain.OfflineSubmission, error) {
	m.switched = append(m.switched, id)
	return domain.OfflineSubmission{TaskID: id, Status: "running", DownloadState: "submitting"}, nil
}

func TestOfflineControlsTargetOneValidatedTask(t *testing.T) {
	m := &controlledOffline{}
	router := gin.New()
	router.Use(errorMiddleware(slog.Default()))
	router.POST("/offline/:id/cancel", offlineControlHandler(m, false))
	router.POST("/offline/:id/next", offlineControlHandler(m, true))
	for _, tc := range []struct {
		path   string
		status int
	}{{"/offline/13/cancel", http.StatusAccepted}, {"/offline/21/next", http.StatusAccepted}, {"/offline/0/cancel", http.StatusBadRequest}, {"/offline/no/next", http.StatusBadRequest}} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, tc.path, nil))
		if w.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
	}
	if len(m.cancelled) != 1 || m.cancelled[0] != 13 || len(m.switched) != 1 || m.switched[0] != 21 {
		t.Fatalf("wrong task controls: %+v", m)
	}
}
