package javlibrary

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ppxb/miyabi/internal/netx"
)

func requireDiagnostic(t *testing.T, err error, operation, category, reason string) *sourceError {
	t.Helper()
	var diagnostic *sourceError
	if !errors.As(err, &diagnostic) || diagnostic.request.Operation != operation || diagnostic.category != category || diagnostic.reason != reason {
		t.Fatalf("diagnostic = %v, want %s/%s/%s", err, operation, category, reason)
	}
	return diagnostic
}

func TestFailureClassificationPreservesCause(t *testing.T) {
	for _, test := range []struct {
		name, category string
		cause          error
	}{
		{"dns", "dns", &net.DNSError{Name: "proxy.invalid", IsNotFound: true}},
		{"proxy", "proxy_connect", &net.OpError{Op: "proxyconnect", Net: "tcp", Err: errors.New("refused")}},
		{"certificate", "tls", x509.UnknownAuthorityError{Cert: &x509.Certificate{}}},
		{"timeout", "timeout", context.DeadlineExceeded},
		{"cancel", "canceled", context.Canceled},
		{"network", "network", errors.New("connection reset")},
	} {
		t.Run(test.name, func(t *testing.T) {
			cause := &url.Error{Op: "Get", URL: "https://example.invalid/?secret=hidden", Err: test.cause}
			d := &requestDiagnostics{started: time.Now(), info: requestInfo{Operation: "search"}}
			err := d.wrap(cause)
			var diagnostic *sourceError
			if !errors.As(err, &diagnostic) || diagnostic.category != test.category || !errors.Is(err, test.cause) {
				t.Fatalf("diagnostic = %v, cause lost or incorrect category", err)
			}
		})
	}
}

func TestFailureTextDoesNotExposeRelativeLocationsOrRawProtocolErrors(t *testing.T) {
	for _, cause := range []error{
		&url.Error{Op: "Get", URL: "/cn/?token=relative-secret", Err: failure("redirect", "redirect_limit")},
		&url.Error{Op: "Get", URL: "https://user:password@example.invalid/?token=absolute-secret", Err: errors.New(`failed to parse Location header "/cn/?token=header-secret"`)},
	} {
		d := &requestDiagnostics{started: time.Now(), info: requestInfo{Operation: "search"}}
		err := d.wrap(cause)
		if !errors.Is(err, cause) {
			t.Fatal("diagnostic discarded the original error")
		}
		for _, secret := range []string{"relative-secret", "absolute-secret", "header-secret", "password"} {
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("failure text exposed %s", secret)
			}
		}
	}
}

func TestDiagnosticsUseActualProxyAndKeepConnectStatus(t *testing.T) {
	proxy := func(status int) *httptest.Server {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodConnect || r.Host != "www.javlibrary.com:443" {
				t.Errorf("unexpected proxy request %s %s", r.Method, r.Host)
			}
			w.WriteHeader(status)
		}))
		t.Cleanup(server.Close)
		return server
	}
	first, second := proxy(407), proxy(403)
	manager, err := netx.NewProxyManager(netx.ProxyConfig{})
	if err != nil {
		t.Fatal(err)
	}
	c := New(manager)
	defer c.Close()
	for _, step := range []struct {
		server *httptest.Server
		status int
	}{{first, 407}, {second, 403}} {
		address, _ := url.Parse(step.server.URL)
		address.User = url.UserPassword("proxy-user", "proxy-secret")
		if err := manager.Update(netx.ProxyConfig{Enabled: true, URL: address.String()}); err != nil {
			t.Fatal(err)
		}
		_, err := c.Lookup(t.Context(), "ABP-123")
		d := requireDiagnostic(t, err, "search", "proxy_http", "connect_rejected")
		if d.request.ProxyStatus != step.status || d.request.Status != 0 || d.request.Proxy != step.server.URL {
			t.Fatalf("CONNECT confused with origin or stale proxy: %+v", d.request)
		}
	}
}

func TestPageDiagnosticsDistinguishStatusAnomalyAndUnknownHTML(t *testing.T) {
	for _, test := range []struct {
		name, body, category, reason string
		status                       int
	}{
		{"status", "not read", "http_status", "non_200_response", 503},
		{"unavailable", "<h1>Site Unavailable</h1>", "unexpected_page", "site_unavailable", 200},
		{"challenge", `<title>Just a moment...</title><form id="challenge-form"></form>`, "unexpected_page", "challenge_page", 200},
		{"unknown", "<html><main>different layout</main></html>", "unexpected_page", "search_container_missing", 200},
		{"large", strings.Repeat("x", maxBody+1), "body_limit", "response_too_large", 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := localClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=UTF-8; private=hidden")
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			})
			_, err := c.Lookup(t.Context(), "ABP-123")
			d := requireDiagnostic(t, err, "search", test.category, test.reason)
			if d.request.Status != test.status || d.request.Proxy != "direct" || d.request.ContentType != "text/html" || strings.Contains(d.request.Target, "?") {
				t.Fatalf("wrong response diagnostics: %+v", d.request)
			}
		})
	}
}

func TestRedirectDiagnosticsRetainExistingLimits(t *testing.T) {
	for _, count := range []int{2, 3} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			requests := 0
			c := localClient(t, func(w http.ResponseWriter, r *http.Request) {
				requests++
				if requests <= count {
					http.Redirect(w, r, fmt.Sprintf("/cn/?v=film&step=%d", requests), 302)
					return
				}
				fmt.Fprint(w, filmHTML)
			})
			film, err := c.Lookup(t.Context(), "ABP-123")
			if count == 2 {
				if err != nil || film.ID != "film" || requests != 3 {
					t.Fatalf("two redirects must work: %+v %v requests=%d", film, err, requests)
				}
			} else {
				d := requireDiagnostic(t, err, "search", "redirect", "redirect_limit")
				if requests != 3 || d.request.Redirects != 3 || d.request.Status != 302 || strings.Contains(d.request.RedirectTarget, "?") {
					t.Fatalf("third redirect followed or metadata wrong: %+v requests=%d", d.request, requests)
				}
			}
		})
	}
}

func TestDetailAndCommentsKeepIdentityFailures(t *testing.T) {
	c := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cn/vl_searchbyid.php" {
			fmt.Fprint(w, `<div class="videos"><a href="?v=film"><span class="id">ABP-123</span></a></div>`)
		} else if r.URL.Query().Get("v") == "film" {
			http.Redirect(w, r, "/cn/?v=other", 302)
		} else {
			fmt.Fprint(w, filmHTML)
		}
	})
	_, err := c.Lookup(t.Context(), "ABP-123")
	requireDiagnostic(t, err, "detail", "identity", "film_redirect_changed_identity")
	_, err = c.Comments(t.Context(), "film", 1)
	requireDiagnostic(t, err, "comments", "identity", "comments_redirect_changed_identity")
}

func TestPageMarkersDoNotChangeEmptyDataOrMatchCommentWords(t *testing.T) {
	for _, body := range []string{
		`<div class="videos"></div>`,
		`<title>Site Unavailable</title><div class="videos"></div>`,
		`<div id="video_comments"><table class="comment"><td class="t">Site Unavailable / Just a moment</td></table></div>`,
		`<title>Just a moment...</title><main>unknown template without challenge markers</main>`,
	} {
		doc, _ := parseDocument([]byte(body))
		if reason := pageProblem(doc); reason != "" {
			t.Fatalf("ordinary/unknown content classified as %s", reason)
		}
	}
	c := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cn/vl_searchbyid.php":
			fmt.Fprint(w, `<div class="videos"></div>`)
		default:
			fmt.Fprint(w, `<div id="video_comments"></div>`)
		}
	})
	film, err := c.Lookup(t.Context(), "ABP-123")
	if err != nil || film.Rating.Found || film.Rating.Max != 10 || film.Rating.Score != nil {
		t.Fatalf("empty search changed: %+v %v", film, err)
	}
	comments, err := c.Comments(t.Context(), "film", 1)
	if err != nil || comments.Items == nil || len(comments.Items) != 0 || comments.HasMore {
		t.Fatalf("empty comments changed: %+v %v", comments, err)
	}
}

func TestDebugPageSummaryNeverLogsBodyQueryOrHeaderParameters(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	c := localClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; private=header-secret")
		w.Header().Set("Set-Cookie", "session=cookie-secret")
		fmt.Fprint(w, `<div class="videos"></div><p>body-secret</p>`)
	})
	if _, err := c.Lookup(t.Context(), "ABP-123"); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"ABP-123", "keyword", "body-secret", "header-secret", "cookie-secret"} {
		if strings.Contains(logs.String(), value) {
			t.Fatalf("private value in debug summary: %s", value)
		}
	}
	if !strings.Contains(logs.String(), `"proxy":"direct"`) || !strings.Contains(logs.String(), "videos:true") || !strings.Contains(logs.String(), `"attempt":`) {
		t.Fatalf("missing route/shape/attempt: %s", logs.String())
	}
}
