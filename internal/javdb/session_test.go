package javdb

import (
	"errors"
	"io"
	"strings"
	"testing"

	http "github.com/bogdanfinn/fhttp"
	"golang.org/x/time/rate"
)

type sessionTestClient func(*http.Request) (*http.Response, error)

func (f sessionTestClient) Do(request *http.Request) (*http.Response, error) { return f(request) }
func (sessionTestClient) CloseIdleConnections()                              {}

func TestSessionLoginRestoresSearchAndDetailScore(t *testing.T) {
	const token = "test.app.token"
	ctx := t.Context()
	client := &Client{limiter: rate.NewLimiter(rate.Inf, 1)}
	state, err := client.installRoute(ctx, RouteStatus{Host: bootstrapHosts[0]})
	if err != nil {
		t.Fatal(err)
	}
	route := state.transport.(*transport)
	route.client = sessionTestClient(func(request *http.Request) (*http.Response, error) {
		var body string
		switch request.URL.Path {
		case "/api/v1/sessions":
			if err := request.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if request.Method != http.MethodPost || request.Form.Get("username") != "test@example.invalid" ||
				request.Form.Get("password") != "test + password" || request.Header.Get("authorization") != "" ||
				request.Header.Get("jdsignature") == "" {
				t.Fatal("login request did not use the signed form protocol")
			}
			body = `{"success":1,"data":{"token":"` + token + `"}}`
		case "/api/v2/search":
			body = string(fixtureFile(t, "search.json"))
		case "/api/v4/movies/movie-exact":
			body = string(fixtureFile(t, "movie.json"))
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
		if request.Method == http.MethodGet && request.Header.Get("authorization") != "Bearer "+token {
			body = `{"success":0,"action":"JWTVerificationError"}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})

	if _, err := client.ResolveMovieID(ctx, "ABP-123"); !errors.Is(err, ErrLoginRequired) {
		t.Fatalf("anonymous search error = %v", err)
	}
	session, err := route.login(ctx, "test@example.invalid", "test + password")
	if err != nil || session != token {
		t.Fatalf("login failed: %v", err)
	}
	client.SetSessionToken(session)
	id, err := client.ResolveMovieID(ctx, "ABP-123")
	if err != nil || id != "movie-exact" {
		t.Fatalf("authenticated resolution = %q, %v", id, err)
	}
	detail, err := client.MovieDetail(ctx, id)
	if err != nil || detail.Rating != 4.5 {
		t.Fatalf("authenticated detail score = %v, %v", detail.Rating, err)
	}

	untrusted, err := client.installRoute(ctx, RouteStatus{Host: "https://discovered.example"})
	if err != nil {
		t.Fatal(err)
	}
	if untrusted.transport.(*transport).token != nil || client.usableRoute() != nil {
		t.Fatal("an untrusted route may use the saved session")
	}
	client.SetSessionToken("")
	if client.usableRoute() != untrusted {
		t.Fatal("removing the session should restore anonymous routing")
	}
}
