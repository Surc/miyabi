package catalogue

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/ppxb/miyabi/internal/domain"
)

type externalRatingStub struct {
	calls, closes atomic.Int32
	load          func(context.Context, string) (domain.ExternalRating, error)
}

func (s *externalRatingStub) Rating(ctx context.Context, code string) (domain.ExternalRating, error) {
	s.calls.Add(1)
	if s.load != nil {
		return s.load(ctx, code)
	}
	return extensionRating(), nil
}

func (s *externalRatingStub) Close() { s.closes.Add(1) }

func extensionRating() domain.ExternalRating {
	score := 8.6
	return domain.ExternalRating{Source: "javlibrary", Max: 10, Found: true, Score: &score}
}

func extrasWithRatingStub(t *testing.T, reader ExternalRatingReader, legacy *socialLibraryStub) *MovieExtras {
	t.Helper()
	extras := NewMovieExtrasWithRating(&Service{}, reader)
	extras.library.Close()
	extras.library = legacy
	t.Cleanup(extras.Close)
	return extras
}

func TestOptionalRatingReaderPreservesLegacyDefault(t *testing.T) {
	legacy := &socialLibraryStub{}
	extras := extrasWithRatingStub(t, nil, legacy)
	rating, err := extras.JavLibraryRating(t.Context(), " abp-123 ")
	if err != nil || rating.Score == nil || *rating.Score != 8.3 {
		t.Fatalf("default rating changed: %+v %v", rating, err)
	}
	if _, err := extras.MovieComments(t.Context(), "javlibrary", "", "ABP-123", 1); err != nil {
		t.Fatal(err)
	}
	if legacy.lookups != 1 || legacy.comments != 1 {
		t.Fatalf("default shared lookup changed: lookups=%d comments=%d", legacy.lookups, legacy.comments)
	}
}

func TestExternalRatingCacheIsIndependentOfLegacyComments(t *testing.T) {
	reader := &externalRatingStub{load: func(_ context.Context, code string) (domain.ExternalRating, error) {
		if code != "ABP-123" {
			t.Errorf("provider received non-normalized code: %q", code)
		}
		return extensionRating(), nil
	}}
	legacy := &socialLibraryStub{fail: true}
	extras := extrasWithRatingStub(t, reader, legacy)
	for _, code := range []string{" abp-123 ", "ABP-123"} {
		rating, err := extras.JavLibraryRating(t.Context(), code)
		if err != nil || rating.Score == nil || *rating.Score != 8.6 {
			t.Fatalf("external rating = %+v, error = %v", rating, err)
		}
	}
	if reader.calls.Load() != 1 || legacy.lookups != 0 {
		t.Fatal("rating failed to use its own normalized cache")
	}
	if _, err := extras.MovieComments(t.Context(), "javlibrary", "", "ABP-123", 1); !domain.IsKind(err, domain.KindUpstream) {
		t.Fatalf("comment lookup failure was hidden: %v", err)
	}
	legacy.fail = false
	comments, err := extras.MovieComments(t.Context(), "javlibrary", "", "ABP-123", 1)
	if err != nil || len(comments.Items) != 1 || legacy.lookups != 2 || legacy.comments != 1 {
		t.Fatalf("legacy comments did not recover independently: %+v %v", comments, err)
	}
	legacy.failComments = true
	if _, err := extras.MovieComments(t.Context(), "javlibrary", "", "ABP-123", 2); !domain.IsKind(err, domain.KindUpstream) {
		t.Fatalf("later comment page failure was hidden: %v", err)
	}
	rating, err := extras.JavLibraryRating(t.Context(), "ABP-123")
	if err != nil || rating.Score == nil || *rating.Score != 8.6 || reader.calls.Load() != 1 {
		t.Fatalf("comment lookup replaced external rating: %+v %v", rating, err)
	}
}

func TestExternalRatingFailureRetriesWithoutAffectingComments(t *testing.T) {
	cause := errors.New("rating service unavailable")
	reader := &externalRatingStub{}
	reader.load = func(context.Context, string) (domain.ExternalRating, error) {
		if reader.calls.Load() <= 2 {
			return domain.ExternalRating{}, cause
		}
		return extensionRating(), nil
	}
	legacy := &socialLibraryStub{}
	extras := extrasWithRatingStub(t, reader, legacy)
	if _, err := extras.MovieComments(t.Context(), "javlibrary", "", "ABP-123", 1); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := extras.JavLibraryRating(t.Context(), "ABP-123"); !domain.IsKind(err, domain.KindUpstream) || !errors.Is(err, cause) {
			t.Fatalf("rating failure classification/cause lost: %v", err)
		}
	}
	for range 2 {
		if rating, err := extras.JavLibraryRating(t.Context(), "ABP-123"); err != nil || rating.Score == nil || *rating.Score != 8.6 {
			t.Fatalf("rating retry did not recover: %+v %v", rating, err)
		}
	}
	if reader.calls.Load() != 3 {
		t.Fatalf("failed load cached or successful retry not cached: %d", reader.calls.Load())
	}
	comments, err := extras.MovieComments(t.Context(), "javlibrary", "", "ABP-123", 1)
	if err != nil || len(comments.Items) != 1 || legacy.lookups != 1 || legacy.comments != 1 {
		t.Fatalf("rating failure affected saved comment page: %+v %v", comments, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := extras.JavLibraryRating(ctx, "ABP-123"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cached result ignored canceled caller: %v", err)
	}
}

func TestExternalRatingSharesLoadWithoutSharingCallerCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		finish := make(chan struct{})
		reader := &externalRatingStub{load: func(ctx context.Context, _ string) (domain.ExternalRating, error) {
			select {
			case <-finish:
				return extensionRating(), nil
			case <-ctx.Done():
				return domain.ExternalRating{}, ctx.Err()
			}
		}}
		extras := extrasWithRatingStub(t, reader, &socialLibraryStub{})
		ctx, cancel := context.WithCancel(t.Context())
		first := make(chan error, 1)
		go func() { _, err := extras.JavLibraryRating(ctx, " abp-123 "); first <- err }()
		synctest.Wait()
		second := make(chan error, 1)
		go func() { _, err := extras.JavLibraryRating(t.Context(), "ABP-123"); second <- err }()
		synctest.Wait()
		cancel()
		if err := <-first; !errors.Is(err, context.Canceled) {
			t.Fatalf("first caller: %v", err)
		}
		close(finish)
		if err := <-second; err != nil || reader.calls.Load() != 1 {
			t.Fatalf("second caller lost shared load: calls=%d error=%v", reader.calls.Load(), err)
		}
	})
}

func TestExternalRatingCancelsAbandonedLoadAndAllowsRetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		abandoned := make(chan struct{})
		reader := &externalRatingStub{}
		reader.load = func(ctx context.Context, _ string) (domain.ExternalRating, error) {
			if reader.calls.Load() == 1 {
				<-ctx.Done()
				close(abandoned)
				return domain.ExternalRating{}, ctx.Err()
			}
			return extensionRating(), nil
		}
		extras := extrasWithRatingStub(t, reader, &socialLibraryStub{})
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { _, err := extras.JavLibraryRating(ctx, "ABP-123"); done <- err }()
		synctest.Wait()
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		<-abandoned
		if _, err := extras.JavLibraryRating(t.Context(), "ABP-123"); err != nil || reader.calls.Load() != 2 {
			t.Fatalf("abandoned load poisoned retry: calls=%d error=%v", reader.calls.Load(), err)
		}
	})
}

type closingSocialStub struct {
	*socialLibraryStub
	closes int
}

func (s *closingSocialStub) Close() { s.closes++ }

func TestMovieExtrasOwnsBothIndependentReaders(t *testing.T) {
	reader := &externalRatingStub{}
	extras := NewMovieExtrasWithRating(&Service{}, reader)
	extras.library.Close()
	legacy := &closingSocialStub{socialLibraryStub: &socialLibraryStub{}}
	extras.library = legacy
	extras.Close()
	if legacy.closes != 1 || reader.closes.Load() != 1 {
		t.Fatalf("reader ownership not closed: legacy=%d external=%d", legacy.closes, reader.closes.Load())
	}
}
