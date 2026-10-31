package javlibrary

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ppxb/miyabi/internal/netx"
	"golang.org/x/time/rate"
)

func nativeRatingClient(t *testing.T, proxy *netx.ProxyManager, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := NewRatingReader(proxy)
	address, _ := url.Parse(server.URL)
	client.base.Scheme, client.base.Host = address.Scheme, address.Host
	client.http.Transport.(*ratingTransport).limiter = rate.NewLimiter(rate.Inf, 1)
	t.Cleanup(client.Close)
	return client
}

func TestNativeRatingPreservesIdentityCookiesAndTenPointScale(t *testing.T) {
	requests := 0
	client := nativeRatingClient(t, nil, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if !strings.Contains(r.UserAgent(), "Chrome/124.") {
			t.Errorf("unexpected initial browser profile: %q", r.UserAgent())
		}
		switch r.URL.Path {
		case "/cn/vl_searchbyid.php":
			if r.URL.Query().Get("keyword") != "ABP-123" {
				t.Error("rating number not normalized")
			}
			http.SetCookie(w, &http.Cookie{Name: "rating-session", Value: "test", Path: "/cn/"})
			fmt.Fprint(w, `<div class="videos"><a href="?v=wrong"><span class="id">ABP-1234</span></a><a href="?v=film"><span class="id">ABP-123</span></a></div>`)
		case "/cn/":
			cookie, err := r.Cookie("rating-session")
			if err != nil || cookie.Value != "test" || r.URL.Query().Get("v") != "film" {
				t.Error("detail lost its session or selected the wrong film")
			}
			fmt.Fprint(w, filmHTML+`<img src="/must-not-fetch.jpg">`)
		default:
			t.Errorf("rating fetched unrelated resource: %s", r.URL)
			w.WriteHeader(404)
		}
	})
	rating, err := client.Rating(t.Context(), " abp-123 ")
	if err != nil || !rating.Found || rating.Source != "javlibrary" || rating.Max != 10 || rating.Score == nil || *rating.Score != 8.25 || requests != 2 {
		t.Fatalf("rating=%+v err=%v requests=%d", rating, err, requests)
	}
}

func TestNativeRatingRemembersAcceptedFingerprint(t *testing.T) {
	for _, status := range []int{403, 406} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			rejected, accepted := 0, 0
			client := nativeRatingClient(t, nil, func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.UserAgent(), "Chrome/124.") {
					rejected++
					http.SetCookie(w, &http.Cookie{Name: "rejected-profile", Value: "test", Path: "/"})
					w.WriteHeader(status)
					return
				}
				accepted++
				if !strings.Contains(r.UserAgent(), "Chrome/131.") {
					t.Errorf("wrong replacement browser: %s", r.UserAgent())
				}
				if _, err := r.Cookie("rejected-profile"); err == nil {
					t.Error("new fingerprint reused rejected cookies")
				}
				if r.URL.Path == "/cn/vl_searchbyid.php" {
					http.Redirect(w, r, "/cn/?v=film", http.StatusFound)
					return
				}
				fmt.Fprint(w, filmHTML)
			})
			for range 2 {
				if _, err := client.Rating(t.Context(), "ABP-123"); err != nil {
					t.Fatal(err)
				}
			}
			if rejected != 1 || accepted != 4 {
				t.Fatalf("fingerprint preference not retained: rejected=%d accepted=%d", rejected, accepted)
			}
		})
	}
}

func TestNativeRatingBoundsBlockedRequestsAndNeverInventsScore(t *testing.T) {
	requests := 0
	client := nativeRatingClient(t, nil, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(403)
		fmt.Fprint(w, `<title>Just a moment...</title>`)
	})
	for range 2 {
		rating, err := client.Rating(t.Context(), "ABP-123")
		if err == nil || rating.Found || rating.Score != nil {
			t.Fatalf("challenge became a rating: %+v %v", rating, err)
		}
		d := requireDiagnostic(t, err, "search", "http_status", "non_200_response")
		if d.request.Status != 403 || d.request.Proxy != "direct" {
			t.Fatalf("lost final status or proxy diagnostics: %+v", d.request)
		}
	}
	if requests != 5 {
		t.Fatalf("blocked profiles retried without a bound: %d", requests)
	}
}

func TestNativeRatingRejectsWrongFilmsPagesAndRedirects(t *testing.T) {
	for _, mode := range []string{"wrong film", "invalid score", "challenge", "foreign redirect", "same-origin loop", "no score"} {
		t.Run(mode, func(t *testing.T) {
			client := nativeRatingClient(t, nil, func(w http.ResponseWriter, r *http.Request) {
				if mode == "foreign redirect" {
					http.Redirect(w, r, "https://example.invalid/", http.StatusFound)
					return
				}
				if r.URL.Path != "/cn/" || mode == "same-origin loop" {
					http.Redirect(w, r, "/cn/?v=film", http.StatusFound)
					return
				}
				body := filmHTML
				switch mode {
				case "wrong film":
					body = strings.ReplaceAll(body, "ABP-123", "ABP-1234")
				case "invalid score":
					body = strings.ReplaceAll(body, "8.25", "99")
				case "challenge":
					body = `<title>Just a moment...</title><form id="challenge-form"></form>`
				case "no score":
					body = strings.ReplaceAll(body, `<span class="score">(8.25)</span>`, "")
				}
				fmt.Fprint(w, body)
			})
			rating, err := client.Rating(t.Context(), "ABP-123")
			if mode == "no score" {
				if err != nil || !rating.Found || rating.Score != nil || rating.Max != 10 {
					t.Fatalf("unrated film changed: %+v %v", rating, err)
				}
			} else if err == nil || rating.Score != nil {
				t.Fatalf("invalid source accepted: %+v %v", rating, err)
			}
		})
	}
}

func TestNativeRatingSwitchesProxyWithoutRestart(t *testing.T) {
	var first, second atomic.Int32
	proxy := func(calls *atomic.Int32) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodConnect {
				t.Error("expected a proxy tunnel")
				w.WriteHeader(405)
				return
			}
			calls.Add(1)
			upstream, err := net.DialTimeout("tcp", r.Host, time.Second)
			if err != nil {
				t.Error(err)
				w.WriteHeader(502)
				return
			}
			defer upstream.Close()
			conn, buffered, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close()
			fmt.Fprint(buffered, "HTTP/1.1 200 Connection Established\r\n\r\n")
			if err := buffered.Flush(); err != nil {
				t.Error(err)
				return
			}
			done := make(chan struct{})
			go func() {
				defer close(done)
				io.Copy(upstream, buffered)
				upstream.Close()
			}()
			io.Copy(conn, upstream)
			conn.Close()
			<-done
		}))
	}
	firstProxy, secondProxy := proxy(&first), proxy(&second)
	t.Cleanup(firstProxy.Close)
	t.Cleanup(secondProxy.Close)
	manager, err := netx.NewProxyManager(netx.ProxyConfig{Enabled: true, URL: firstProxy.URL})
	if err != nil {
		t.Fatal(err)
	}
	client := nativeRatingClient(t, manager, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cn/vl_searchbyid.php" {
			http.Redirect(w, r, "/cn/?v=film", http.StatusFound)
			return
		}
		fmt.Fprint(w, filmHTML)
	})
	for _, address := range []string{firstProxy.URL, secondProxy.URL, ""} {
		if err := manager.Update(netx.ProxyConfig{Enabled: address != "", URL: address}); err != nil {
			t.Fatal(err)
		}
		if _, err := client.Rating(t.Context(), "ABP-123"); err != nil {
			t.Fatal(err)
		}
	}
	if first.Load() != 1 || second.Load() != 1 {
		t.Fatalf("proxy changes not applied: first=%d second=%d", first.Load(), second.Load())
	}
}

func TestNativeRatingCancellationStopsRateLimitedRequest(t *testing.T) {
	client := nativeRatingClient(t, nil, func(http.ResponseWriter, *http.Request) {
		t.Error("canceled request reached the site")
	})
	client.http.Transport.(*ratingTransport).limiter = rate.NewLimiter(rate.Every(time.Hour), 1)
	client.http.Transport.(*ratingTransport).limiter.Allow()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := client.Rating(ctx, "ABP-123"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}
