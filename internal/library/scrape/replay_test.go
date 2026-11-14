package scrape

import (
	"context"
	"encoding/json"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/tasks"
	"testing"
)

func TestCompletedScrapeReplayNeedsNoExternalServices(t *testing.T) {
	service := &Service{}
	for _, completed := range []bool{true, false} {
		body, err := tasks.EncodePayload(Payload{Completed: completed})
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			err := service.Scrape(t.Context(), tasks.Job{ID: 1, Payload: body})
			if completed && err != nil || !completed && !domain.IsKind(err, domain.KindInvalid) {
				t.Fatalf("replay completed=%t: %v", completed, err)
			}
		}
	}
	if err := service.Scrape(t.Context(), tasks.Job{Payload: json.RawMessage(`{"completed":`)}); err == nil {
		t.Fatal("malformed checkpoint accepted")
	}
}

func TestAttemptHookRejectsUnstampedCandidatesAndSkipsCompleted(t *testing.T) {
	calls := 0
	service := &Service{beginAttempt: func(ctx context.Context, old string) (context.Context, string, error) {
		calls++
		return ctx, "attempt", nil
	}}
	for _, input := range []Payload{{Completed: true}, {MetadataReady: true}} {
		body, err := tasks.EncodePayload(input)
		if err != nil {
			t.Fatal(err)
		}
		err = service.Scrape(t.Context(), tasks.Job{ID: 1, Payload: body})
		if input.Completed && err != nil || !input.Completed && !domain.IsKind(err, domain.KindConflict) {
			t.Fatalf("unexpected replay result: %v", err)
		}
	}
	if calls != 0 {
		t.Fatal("completed or unverifiable replay reached online snapshot")
	}
}
