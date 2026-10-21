package javlibrary

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func localClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c := New(nil)
	u, _ := url.Parse(server.URL)
	c.base.Scheme, c.base.Host = u.Scheme, u.Host
	t.Cleanup(c.Close)
	return c
}

func TestLookupSearchFollowAndComments(t *testing.T) {
	c := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cn/vl_searchbyid.php":
			if r.URL.Query().Get("keyword") != "ABP-123" {
				t.Error("code not normalized")
			}
			fmt.Fprint(w, `<div class="videos"><a href="?v=film"><span class="id">ABP-123</span></a></div>`)
		case "/cn/":
			if r.URL.Query().Get("v") != "film" {
				t.Error("wrong film")
			}
			fmt.Fprint(w, filmHTML)
		case "/cn/videocomments.php":
			if r.URL.Query().Get("page") != "2" {
				t.Error("wrong page")
			}
			fmt.Fprint(w, `<div id="video_comments"></div>`)
		default:
			t.Errorf("unexpected request %s", r.URL)
			w.WriteHeader(404)
		}
	})
	film, err := c.Lookup(t.Context(), " abp-123 ")
	if err != nil || film.ID != "film" || film.Rating.Score == nil {
		t.Fatalf("%+v %v", film, err)
	}
	page, err := c.Comments(t.Context(), film.ID, 2)
	if err != nil || page.Page != 2 || page.Items == nil {
		t.Fatalf("%+v %v", page, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.Lookup(ctx, "ABP-123"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled=%v", err)
	}
}

func TestClientRejectsErrorsAndRedirects(t *testing.T) {
	for _, mode := range []string{"status", "oversize", "foreign redirect", "changed film", "challenge"} {
		t.Run(mode, func(t *testing.T) {
			c := localClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "status":
					w.WriteHeader(503)
				case "oversize":
					fmt.Fprint(w, strings.Repeat("x", maxBody+1))
				case "foreign redirect":
					http.Redirect(w, r, "https://example.invalid/", 302)
				case "changed film":
					http.Redirect(w, r, "/cn/?v=other", 302)
				case "challenge":
					fmt.Fprint(w, `<h1>Site unavailable</h1>`)
				}
			})
			if _, err := c.Lookup(t.Context(), "ABP-123"); err == nil {
				t.Fatal("invalid upstream response accepted")
			}
		})
	}
}
