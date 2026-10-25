package catalogue

import (
	"context"
	"fmt"
	"testing"

	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/javlibrary"
)

type socialLibraryStub struct {
	lookups, comments int
	fail              bool
	failComments      bool
}

func (s *socialLibraryStub) Lookup(ctx context.Context, code string) (javlibrary.Film, error) {
	s.lookups++
	if err := ctx.Err(); err != nil {
		return javlibrary.Film{}, err
	}
	if s.fail {
		return javlibrary.Film{}, fmt.Errorf("upstream failure")
	}
	score := 8.3
	return javlibrary.Film{ID: code, Rating: domain.ExternalRating{Source: "javlibrary", Max: 10, Found: true, Score: &score}}, nil
}
func (s *socialLibraryStub) Comments(_ context.Context, id string, page int) (domain.MovieComments, error) {
	s.comments++
	if s.failComments {
		// Parsing can produce some rows before encountering a malformed one.
		return domain.MovieComments{Source: "javlibrary", Page: page, Items: []domain.MovieComment{{ID: "partial", Content: "incomplete"}}}, fmt.Errorf("invalid source comments page")
	}
	return domain.MovieComments{Source: "javlibrary", Page: page, Items: []domain.MovieComment{{ID: id, Content: "comment"}}}, nil
}
func (*socialLibraryStub) Close() {}

func TestExtrasCacheSharesLookupAndIsolatesPages(t *testing.T) {
	e := NewMovieExtras(&Service{})
	defer e.Close()
	stub := &socialLibraryStub{}
	e.library = stub
	rating, err := e.JavLibraryRating(t.Context(), " abp-123 ")
	if err != nil || rating.Max != 10 {
		t.Fatal(rating, err)
	}
	for _, page := range []int{1, 1, 2} {
		if _, err := e.MovieComments(t.Context(), "javlibrary", "", "ABP-123", page); err != nil {
			t.Fatal(err)
		}
	}
	if stub.lookups != 1 || stub.comments != 2 {
		t.Fatalf("lookups=%d comments=%d", stub.lookups, stub.comments)
	}
	if _, err := e.JavLibraryRating(t.Context(), "ABP-124"); err != nil {
		t.Fatal(err)
	}
	if stub.lookups != 2 {
		t.Fatal("different film reused lookup")
	}
	if _, err := e.MovieComments(t.Context(), "javdb", "film", "", 1); !domain.IsKind(err, domain.KindUpstream) {
		t.Fatal("missing optional capability was not isolated", err)
	}
}

func TestExtrasNeverCachesFailureAndPropagatesCancellation(t *testing.T) {
	e := NewMovieExtras(&Service{})
	defer e.Close()
	stub := &socialLibraryStub{fail: true}
	e.library = stub
	for range 2 {
		if _, err := e.JavLibraryRating(t.Context(), "ABP-123"); !domain.IsKind(err, domain.KindUpstream) {
			t.Fatal(err)
		}
	}
	if stub.lookups != 2 {
		t.Fatal("failure cached")
	}
	stub.fail = false
	if _, err := e.JavLibraryRating(t.Context(), "ABP-123"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := e.JavLibraryRating(ctx, "ABP-123"); err != context.Canceled {
		t.Fatal(err)
	}
}

func TestExtrasCommentFailureRetriesWithoutLosingEarlierPage(t *testing.T) {
	e := NewMovieExtras(&Service{})
	defer e.Close()
	stub := &socialLibraryStub{}
	e.library = stub
	load := func(page int) (domain.MovieComments, error) {
		return e.MovieComments(t.Context(), "javlibrary", "", "ABP-123", page)
	}
	if _, err := load(1); err != nil {
		t.Fatal(err)
	}
	stub.failComments = true
	for range 2 {
		if _, err := load(2); !domain.IsKind(err, domain.KindUpstream) {
			t.Fatalf("source failure became empty success: %v", err)
		}
	}
	if stub.comments != 3 {
		t.Fatal("failed second page was cached")
	}
	first, err := load(1)
	if err != nil || first.Page != 1 || len(first.Items) != 1 || stub.comments != 3 {
		t.Fatalf("successful first page lost: %+v %v", first, err)
	}
	stub.failComments = false
	for range 2 {
		second, err := load(2)
		if err != nil || second.Page != 2 || len(second.Items) != 1 {
			t.Fatalf("second page retry failed: %+v %v", second, err)
		}
	}
	if stub.lookups != 1 || stub.comments != 4 {
		t.Fatalf("lookup not shared or successful retry not cached: lookups=%d comments=%d", stub.lookups, stub.comments)
	}
}
