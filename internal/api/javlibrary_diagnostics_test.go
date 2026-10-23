package api

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/javlibrary"
	"github.com/ppxb/miyabi/internal/netx"
)

type javLibraryDiagnosticManager struct {
	MovieExtrasManager
	client *javlibrary.Client
}

func (m *javLibraryDiagnosticManager) JavLibraryRating(ctx context.Context, code string) (domain.ExternalRating, error) {
	film, err := m.client.Lookup(ctx, code)
	if err != nil {
		return film.Rating, domain.E(domain.KindUpstream, "外部评分或评论暂不可用，请稍后重试", err)
	}
	return film.Rating, nil
}

// A real adapter/CONNECT failure must retain diagnostics only in the existing
// sanitized access log. No upstream connection is made by this fixture.
func TestJavLibraryDiagnosticsStayInSanitizedAccessLog(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			t.Errorf("unexpected method %s", r.Method)
		}
		w.WriteHeader(http.StatusProxyAuthRequired)
	}))
	defer proxy.Close()
	address, _ := url.Parse(proxy.URL)
	address.User = url.UserPassword("diagnostic-user", "diagnostic-password")
	manager, err := netx.NewProxyManager(netx.ProxyConfig{Enabled: true, URL: address.String()})
	if err != nil {
		t.Fatal(err)
	}
	client := javlibrary.New(manager)
	defer client.Close()
	for _, incomingID := range []string{"", "javlibrary-diagnostic-test"} {
		t.Run("incoming="+incomingID, func(t *testing.T) {
			var logs bytes.Buffer
			router := NewRouter(Dependencies{
				Logger:      slog.New(slog.NewJSONHandler(&logs, nil)),
				Access:      NewAccessGateService("", ""),
				MovieExtras: &javLibraryDiagnosticManager{client: client},
			})
			request := httptest.NewRequest(http.MethodGet, "/api/extensions/movie/javlibrary-rating?code=ABP-123", nil)
			if incomingID != "" {
				request.Header.Set("X-Request-Id", incomingID)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusBadGateway || response.Body.String() != `{"error":"外部评分或评论暂不可用，请稍后重试"}` {
				t.Fatalf("public response changed: %d %s", response.Code, response.Body.String())
			}
			// slog-gin returns generated IDs in the response and preserves an
			// incoming ID in the log without requiring a response-header echo.
			wantID := incomingID
			if wantID == "" {
				wantID = response.Header().Get("X-Request-Id")
			}
			records := readLogRecords(t, logs.Bytes())
			if wantID == "" || len(records) != 1 || records[0]["id"] != wantID {
				t.Fatalf("request correlation lost or duplicate failure logs: %s", logs.String())
			}
			for _, field := range []string{"operation=search", "category=proxy_http", "proxy_status=407"} {
				if !strings.Contains(logs.String(), field) || strings.Contains(response.Body.String(), field) {
					t.Fatalf("diagnostic missing from logs or exposed to API: %s", field)
				}
			}
			assertPrivateLogValuesAbsent(t, logs.String(), "diagnostic-user", "diagnostic-password", "ABP-123")
		})
	}
}
