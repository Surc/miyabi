package app

import (
	"testing"

	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/domain"
	subtitlemeta "github.com/ppxb/miyabi/internal/domain/subtitle"
	"github.com/ppxb/miyabi/internal/ent/task"
	"github.com/ppxb/miyabi/internal/library/scrape"
	"github.com/ppxb/miyabi/internal/tasks"
)

func TestSubtitleTaskProjectionKeepsOldFailuresVisibleAndRetriesOnce(t *testing.T) {
	ctx := t.Context()
	store, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	service := tasks.NewService(store.Client, tasks.NewRegistry())
	views := &taskViews{Service: service, database: store.Client}
	body, err := tasks.EncodePayload(scrape.SubtitlePayload{MovieID: 7, Target: subtitlemeta.Target{
		Code: "ABP-123", FileID: "video", Dir: "/exports/ABP/ABP-123", Stem: "ABP-123",
	}})
	if err != nil {
		t.Fatal(err)
	}
	failed := store.Client.Task.Create().SetType(string(tasks.KindSubtitle)).SetPayload(body).
		SetStatus(task.StatusFailed).SetResourceKey("subtitle:fixture").SetRetryCount(tasks.MaxRetries).SetError("source unavailable").SaveX(ctx)
	for range 25 {
		store.Client.Task.Create().SetType(string(tasks.KindSubtitle)).SetPayload(body).SetStatus(task.StatusDone).SaveX(ctx)
	}
	items, err := views.listSubtitleTasks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range items {
		if item.ID == failed.ID {
			found = true
			if !item.CanRetry || item.Code != "ABP-123" || item.MovieID != 7 || item.Type != "subtitle" {
				t.Fatalf("wrong projection: %+v", item)
			}
		}
	}
	if !found {
		t.Fatal("older failure hidden by successful history")
	}
	before := service.Version()
	result, err := views.Retry(ctx, failed.ID)
	if err != nil || result.Status != "queued" || result.CanRetry || result.RetryCount != 0 || result.Error != nil || service.Version() == before {
		t.Fatalf("retry did not reset and publish state: %+v %v", result, err)
	}
	if _, err := views.Retry(ctx, failed.ID); !domain.IsKind(err, domain.KindConflict) {
		t.Fatalf("duplicate retry accepted: %v", err)
	}
}
