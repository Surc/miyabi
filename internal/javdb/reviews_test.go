package javdb

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMovieCommentsUsesSignedClientAndNativeScale(t *testing.T) {
	transport := &fixtureTransport{responses: map[string][]byte{"/api/v1/movies/film/reviews": []byte(`{"success":true,"data":{"reviews":[{"id":123,"username":"reader","content":"hello","created_at":"2026-10-21","score":"4.5","likes_count":3},null,{"id":"456","content":" "}]}}`)}}
	page, err := clientWithTransport(transport).MovieComments(t.Context(), "film", 2)
	if err != nil || len(page.Items) != 1 || page.HasMore || page.Items[0].ID != "123" || *page.Items[0].Rating != 4.5 || page.Items[0].RatingMax != 5 || *page.Items[0].Likes != 3 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	if transport.calls[0].params.Get("page") != "2" || transport.calls[0].params.Get("limit") != "20" {
		t.Fatalf("call=%+v", transport.calls[0])
	}
}

func TestMovieCommentsEmptyMalformedAndPagination(t *testing.T) {
	for _, test := range []struct {
		data  string
		valid bool
	}{
		{`{"reviews":null}`, true}, {`{"reviews":[]}`, true}, {`{}`, false}, {`{"reviews":{}}`, false}, {`{"reviews":[{"content":"hello"}]}`, false},
	} {
		transport := &fixtureTransport{responses: map[string][]byte{"/api/v1/movies/film/reviews": []byte(`{"success":true,"data":` + test.data + `}`)}}
		page, err := clientWithTransport(transport).MovieComments(t.Context(), "film", 1)
		if (err == nil) != test.valid {
			t.Fatalf("%s: page=%+v err=%v", test.data, page, err)
		}
	}
	records := make([]map[string]any, 20)
	for i := range records {
		records[i] = map[string]any{"id": i + 1, "content": ""}
	}
	raw, _ := json.Marshal(records)
	transport := &fixtureTransport{responses: map[string][]byte{"/api/v1/movies/film/reviews": []byte(`{"success":true,"data":{"reviews":` + string(raw) + `}}`)}}
	p, err := clientWithTransport(transport).MovieComments(t.Context(), "film", 1)
	if err != nil || !p.HasMore || len(p.Items) != 0 {
		t.Fatalf("rating-only pagination=%+v %v", p, err)
	}
	for _, id := range []string{"../film", "film?x=1", strings.Repeat("a", 201)} {
		if _, err := clientWithTransport(transport).MovieComments(t.Context(), id, 1); err == nil {
			t.Fatal("invalid id accepted")
		}
	}
	for _, page := range []int{0, -1, 1001} {
		if _, err := clientWithTransport(transport).MovieComments(t.Context(), "film", page); err == nil {
			t.Fatal("invalid page accepted")
		}
	}
}
