package catalogue

import (
	"context"
	"errors"
	"testing"

	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/javlibrary"
)

func TestBrowserVerificationFailureIsActionableAndNotCached(t *testing.T) {
	reader := &externalRatingStub{}
	reader.load = func(context.Context, string) (domain.ExternalRating, error) {
		if reader.calls.Load() == 1 {
			return domain.ExternalRating{}, javlibrary.ErrVerificationRequired
		}
		return extensionRating(), nil
	}
	extras := extrasWithRatingStub(t, reader, &socialLibraryStub{})
	_, err := extras.JavLibraryRating(t.Context(), "ABP-123")
	if !domain.IsKind(err, domain.KindUpstream) || !errors.Is(err, javlibrary.ErrVerificationRequired) {
		t.Fatalf("verification status lost: %v", err)
	}
	rating, err := extras.JavLibraryRating(t.Context(), "ABP-123")
	if err != nil || rating.Score == nil || reader.calls.Load() != 2 {
		t.Fatalf("confirmed session could not retry: %+v %v", rating, err)
	}
}
