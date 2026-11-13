package javdb

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	http "github.com/bogdanfinn/fhttp"
)

func TestParseWebMovieDetail(t *testing.T) {
	body := fixtureFile(t, "web_movie.html")
	detail, err := parseWebMovieDetail(body, "QD3n9")
	if err != nil {
		t.Fatal(err)
	}
	if detail.ID != "QD3n9" || detail.Code != "IPX-811" || detail.Title == "" ||
		detail.Rating != 4.71 || detail.RatingMax != 5 || detail.RatingSource != "javdb" {
		t.Fatalf("identity or rating = %#v", detail.Movie)
	}
	if detail.Cover == "" || detail.Duration != 110 || detail.ReleaseDate != "2022-02-08" ||
		len(detail.Tags) != 3 || len(detail.Actors) != 2 || detail.Maker == nil || detail.Series == nil || detail.Director == nil {
		t.Fatalf("metadata = %#v", detail.Movie)
	}
	if len(detail.PreviewImages) != 2 || len(detail.ActorMovies) != 2 || len(detail.RelatedMovies) != 1 {
		t.Fatalf("related data = %#v", detail)
	}

	wrongID := strings.Replace(string(body), "<body>", `<head><link rel="canonical" href="https://javdb.com/v/other"></head><body>`, 1)
	for name, page := range map[string]string{
		"different canonical ID": wrongID,
		"challenge":              "<html><head><title>Just a moment...</title></head></html>",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseWebMovieDetail([]byte(page), "QD3n9"); err == nil {
				t.Fatal("accepted a page that did not identify the requested film")
			}
		})
	}
}

func TestFetchWebMovieUsesPublicPage(t *testing.T) {
	fixture := string(fixtureFile(t, "web_movie.html"))
	client := sessionTestClient(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.String() != "https://javdb.com/v/QD3n9" ||
			request.Header.Get("Authorization") != "" || request.Header.Get("jdsignature") != "" {
			t.Fatalf("unexpected public detail request: %s %s", request.Method, request.URL)
		}
		header := http.Header{"Content-Type": {"text/html; charset=utf-8"}}
		return &http.Response{StatusCode: 200, Header: header, Body: io.NopCloser(strings.NewReader(fixture))}, nil
	})
	detail, err := fetchWebMovie(t.Context(), client, "QD3n9")
	if err != nil || detail.Code != "IPX-811" || detail.Rating != 4.71 {
		t.Fatalf("public detail code=%q rating=%v error=%v", detail.Code, detail.Rating, err)
	}
}

func TestRequiresWebFallback(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want bool
	}{
		{"anonymous", &APIError{Action: "JWTVerificationError"}, true},
		{"expired session", &APIError{Action: "TokenExpired"}, true},
		{"HTTP 401", &HTTPError{StatusCode: 401}, true},
		{"HTTP 403", &HTTPError{StatusCode: 403}, true},
		{"missing movie", &HTTPError{StatusCode: 404}, false},
		{"rate limited", &HTTPError{StatusCode: 429}, false},
		{"ordinary protocol error", &APIError{Action: "NotFound"}, false},
		{"unexpected failure", errors.New("test"), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := requiresWebFallback(test.err); got != test.want {
				t.Fatalf("requiresWebFallback = %t, want %t", got, test.want)
			}
		})
	}
}

func TestFailedReselectionPreservesForbiddenForWebFallback(t *testing.T) {
	forbidden := &HTTPError{StatusCode: http.StatusForbidden}
	routeErr := errors.New("no replacement route")
	client := clientWithTransport(&stubTransport{err: forbidden})
	client.routeContext = t.Context()
	client.selector = func(context.Context, routeSelection) (*routeState, error) {
		return nil, routeErr
	}
	_, err := client.MovieDetail(t.Context(), "QD3n9")
	if !requiresWebFallback(err) || !errors.Is(err, routeErr) {
		t.Fatalf("missing original rejection or route error: %v", err)
	}
}
