package javinizer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ppxb/miyabi/internal/domain"
)

const testToken = "jv_test_token"

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type observedBody struct {
	io.ReadCloser
	started chan struct{}
}

func (b *observedBody) Read(p []byte) (int, error) {
	if b.started != nil {
		close(b.started)
		b.started = nil
	}
	return b.ReadCloser.Read(p)
}

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := New(Config{Enabled: true, ServerURL: server.URL, APIToken: testToken})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client
}

func validResponse() map[string]any {
	return map[string]any{
		"cached": false, "sources_used": 1,
		"movie": map[string]any{
			"code": "ABP-123", "id": "ABP-123", "source_name": "javlibrary", "rating_score": 8.4,
		},
	}
}

func writeResponse(w http.ResponseWriter, response any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Miyabi-JavLibrary-Identity", "page-v1")
	_ = json.NewEncoder(w).Encode(response)
}

func TestRatingRequestsOnlyFreshJavLibraryData(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.RequestURI() != "/api/v1/scrape" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.RequestURI())
		}
		if r.Header.Get("Authorization") != "Bearer "+testToken || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Accept") != "application/json" {
			t.Error("missing API authentication or JSON headers")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		want := map[string]any{"id": "ABP-123", "force": true, "selected_scrapers": []any{"javlibrary"}}
		if !reflect.DeepEqual(body, want) {
			t.Errorf("unexpected scrape request: %#v", body)
		}
		response := validResponse()
		response["future_field"] = "unrelated"
		writeResponse(w, response)
	})
	rating, err := client.Rating(t.Context(), " abp-123 ")
	if err != nil || !rating.Found || rating.Source != "javlibrary" || rating.Max != 10 || rating.Score == nil || *rating.Score != 8.4 {
		t.Fatalf("rating = %+v, error = %v", rating, err)
	}
}

func TestRatingRejectsAmbiguousOrUntrustedResponses(t *testing.T) {
	cases := []struct {
		name   string
		change func(map[string]any, map[string]any)
	}{
		{"missing cache provenance", func(r, _ map[string]any) { delete(r, "cached") }},
		{"cached response", func(r, _ map[string]any) { r["cached"] = true }},
		{"missing source count", func(r, _ map[string]any) { delete(r, "sources_used") }},
		{"multiple sources", func(r, _ map[string]any) { r["sources_used"] = 2 }},
		{"partial error", func(r, _ map[string]any) { r["errors"] = []string{"private error"} }},
		{"missing movie", func(r, _ map[string]any) { delete(r, "movie") }},
		{"null movie", func(r, _ map[string]any) { r["movie"] = nil }},
		{"other rating source", func(_, m map[string]any) { m["source_name"] = "javdb" }},
		{"wrong product code", func(_, m map[string]any) { m["code"] = "ABP-124" }},
		{"wrong secondary identity", func(_, m map[string]any) { m["id"] = "ABP-124" }},
		{"missing product code", func(_, m map[string]any) { delete(m, "code") }},
		{"missing secondary identity", func(_, m map[string]any) { delete(m, "id") }},
		{"missing score", func(_, m map[string]any) { delete(m, "rating_score") }},
		{"null score", func(_, m map[string]any) { m["rating_score"] = nil }},
		{"unrated or failed parse", func(_, m map[string]any) { m["rating_score"] = 0 }},
		{"negative score", func(_, m map[string]any) { m["rating_score"] = -1 }},
		{"different scale", func(_, m map[string]any) { m["rating_score"] = 84 }},
		{"score warning", func(_, m map[string]any) { m["rating_warning"] = "private warning" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := validResponse()
			tc.change(response, response["movie"].(map[string]any))
			client := testClient(t, func(w http.ResponseWriter, _ *http.Request) { writeResponse(w, response) })
			rating, err := client.Rating(t.Context(), "ABP-123")
			if err == nil || rating.Found || rating.Score != nil {
				t.Fatalf("unsafe response accepted: %+v, %v", rating, err)
			}
			if strings.Contains(err.Error(), "private") {
				t.Fatalf("upstream response leaked: %v", err)
			}
		})
	}
}

func TestRatingRejectsUnsupportedAndMalformedHTTPResponses(t *testing.T) {
	cases := []struct {
		name   string
		status int
		header string
		body   string
	}{
		{"vanilla engine", 200, "", `{"cached":false,"sources_used":1,"movie":{"code":"ABP-123","id":"ABP-123","source_name":"javlibrary","rating_score":8.4}}`},
		{"unknown contract", 200, "page-v2", `{}`},
		{"not found or blocked", 404, "page-v1", `{"error":"private upstream detail"}`},
		{"unauthorized", 401, "", "private credentials"},
		{"engine failure", 500, "", "private engine detail"},
		{"html challenge", 200, "page-v1", "<html>private challenge</html>"},
		{"concatenated JSON", 200, "page-v1", `{} {}`},
		{"wrong JSON type", 200, "page-v1", `[]`},
		{"oversized body", 200, "page-v1", strings.Repeat(" ", maxBody+1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-Miyabi-JavLibrary-Identity", tc.header)
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})
			rating, err := client.Rating(t.Context(), "ABP-123")
			if err == nil || rating.Found || rating.Score != nil {
				t.Fatalf("failure became a valid result: %+v, %v", rating, err)
			}
			if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), testToken) {
				t.Fatalf("upstream detail leaked: %v", err)
			}
		})
	}
}

func TestRatingDoesNotFollowRedirectsOrSendCredentials(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Add(1)
		t.Error("redirect destination was contacted")
		writeResponse(w, validResponse())
	}))
	defer target.Close()
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/?token=private", http.StatusTemporaryRedirect)
	})
	_, err := client.Rating(t.Context(), "ABP-123")
	if err == nil || redirected.Load() != 0 || strings.Contains(err.Error(), "private") {
		t.Fatalf("redirect was not isolated: count=%d error=%v", redirected.Load(), err)
	}
}

func TestRatingPreservesTransportCauseWithoutExposingItsText(t *testing.T) {
	client := testClient(t, func(http.ResponseWriter, *http.Request) {
		t.Error("failed transport must not reach the server")
	})
	cause := errors.New("private transport detail containing " + testToken)
	client.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, cause
	})
	_, err := client.Rating(t.Context(), "ABP-123")
	if !errors.Is(err, cause) {
		t.Fatalf("transport cause was lost: %v", err)
	}
	if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), testToken) {
		t.Fatalf("transport detail leaked: %v", err)
	}
}

func TestRatingInvalidInputMakesNoRequest(t *testing.T) {
	var requests atomic.Int32
	client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writeResponse(w, validResponse())
	})
	for _, code := range []string{"", "https://example.com/ABP-123", "ABP-123/movie", strings.Repeat("A", 201)} {
		if _, err := client.Rating(t.Context(), code); !domain.IsKind(err, domain.KindInvalid) {
			t.Errorf("%q: expected invalid input, got %v", code, err)
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("invalid input made %d requests", requests.Load())
	}
}

func TestRatingQueuedCancellationDoesNotCancelActiveRequest(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var requests atomic.Int32
	client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			close(entered)
			<-release
		}
		writeResponse(w, validResponse())
	})
	active := make(chan error, 1)
	go func() { _, err := client.Rating(t.Context(), "ABP-123"); active <- err }()
	<-entered
	ctx, cancel := context.WithCancel(t.Context())
	queued := make(chan error, 1)
	go func() { _, err := client.Rating(ctx, "ABP-123"); queued <- err }()
	cancel()
	if err := <-queued; !errors.Is(err, context.Canceled) {
		t.Errorf("queued request: %v", err)
	}
	if requests.Load() != 1 {
		t.Errorf("queued cancellation made another request: %d", requests.Load())
	}
	close(release)
	if err := <-active; err != nil {
		t.Fatalf("active request affected by queued cancellation: %v", err)
	}
	if _, err := client.Rating(t.Context(), "ABP-123"); err != nil || requests.Load() != 2 {
		t.Fatalf("slot was not released: requests=%d error=%v", requests.Load(), err)
	}
}

func TestRatingInFlightCancellationReleasesSlot(t *testing.T) {
	entered, canceled := make(chan struct{}), make(chan struct{})
	var requests atomic.Int32
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if requests.Add(1) == 1 {
			close(entered)
			<-r.Context().Done()
			close(canceled)
			return
		}
		writeResponse(w, validResponse())
	})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := client.Rating(ctx, "ABP-123"); done <- err }()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("in-flight cancellation: %v", err)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("HTTP request was not canceled")
	}
	if _, err := client.Rating(t.Context(), "ABP-123"); err != nil {
		t.Fatalf("request after cancellation: %v", err)
	}
}

func TestRatingTimeoutIncludesQueue(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var requests atomic.Int32
	client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			close(entered)
			<-release
		}
		writeResponse(w, validResponse())
	})
	active := make(chan error, 1)
	go func() { _, err := client.Rating(t.Context(), "ABP-123"); active <- err }()
	<-entered
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := client.Rating(ctx, "ABP-123"); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("queue timeout: %v", err)
	}
	if requests.Load() != 1 {
		t.Error("timed out queued request reached the engine")
	}
	close(release)
	if err := <-active; err != nil {
		t.Fatalf("active request affected by queue timeout: %v", err)
	}
}

func TestRatingInFlightTimeoutCancelsHTTPRequest(t *testing.T) {
	canceled := make(chan struct{})
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
		close(canceled)
	})
	client.timeout = 50 * time.Millisecond
	if _, err := client.Rating(t.Context(), "ABP-123"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("request timeout: %v", err)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("timed out HTTP request was not canceled")
	}
}

func TestRatingCancellationDuringBodyReadPreservesCause(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(map[bool]string{false: "canceled", true: "deadline"}[timeout], func(t *testing.T) {
			reading := make(chan struct{})
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("X-Miyabi-JavLibrary-Identity", "page-v1")
				_, _ = io.WriteString(w, `{"cached":`)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			})
			transport := client.http.Transport
			client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				response, err := transport.RoundTrip(r)
				if err == nil {
					response.Body = &observedBody{ReadCloser: response.Body, started: reading}
				}
				return response, err
			})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			want := context.Canceled
			if timeout {
				client.timeout = 50 * time.Millisecond
				want = context.DeadlineExceeded
			}
			done := make(chan error, 1)
			go func() { _, err := client.Rating(ctx, "ABP-123"); done <- err }()
			<-reading
			if !timeout {
				cancel()
			}
			if err := <-done; !errors.Is(err, want) {
				t.Fatalf("stream cancellation lost cause: want %v, got %v", want, err)
			}
		})
	}
}
