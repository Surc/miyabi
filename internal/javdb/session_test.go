package javdb

import (
	"context"
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

func TestSessionStatusChecksSavedToken(t *testing.T) {
	const token = "test.app.token"
	for _, test := range []struct {
		name     string
		status   int
		body     string
		expected SessionStatus
	}{
		{name: "no session", expected: SessionSignedOut},
		{name: "verified", status: 200, body: `{"success":1,"data":{"user":{"id":1}}}`, expected: SessionSignedIn},
		{name: "rejected token", status: 200, body: `{"success":0,"action":"JWTVerificationError"}`, expected: SessionExpired},
		{name: "unauthorized", status: 401, expected: SessionExpired},
		{name: "blocked route", status: 403, expected: SessionUnverified},
		{name: "missing user", status: 200, body: `{"success":1,"data":{}}`, expected: SessionUnverified},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := t.Context()
			client := &Client{
				limiter:      rate.NewLimiter(rate.Inf, 1),
				routeContext: ctx,
				selector: func(context.Context, routeSelection) (*routeState, error) {
					return nil, errors.New("no available route")
				},
			}
			state, err := client.installRoute(ctx, RouteStatus{Host: bootstrapHosts[0]})
			if err != nil {
				t.Fatal(err)
			}
			state.transport.(*transport).client = sessionTestClient(func(request *http.Request) (*http.Response, error) {
				if test.status == 0 {
					t.Fatal("session status should make no request without a token")
				}
				if request.Method != http.MethodGet || request.URL.Path != "/api/v1/users" || request.Header.Get("authorization") != "Bearer "+token {
					t.Fatal("session status should check the authenticated user endpoint")
				}
				return &http.Response{StatusCode: test.status, Body: io.NopCloser(strings.NewReader(test.body))}, nil
			})
			if test.status != 0 {
				client.SetSessionToken(token)
			}
			if got := client.SessionStatus(ctx); got != test.expected {
				t.Fatalf("session status = %q, want %q", got, test.expected)
			}
			if client.HasSession() != (test.status != 0) {
				t.Fatal("session status changed the saved token")
			}
		})
	}
}

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
