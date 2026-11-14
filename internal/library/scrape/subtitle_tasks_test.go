package scrape

import (
	"context"
	"errors"
	"testing"

	"github.com/ppxb/miyabi/internal/database"
	subtitlemeta "github.com/ppxb/miyabi/internal/domain/subtitle"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/task"
	"github.com/ppxb/miyabi/internal/export"
	"github.com/ppxb/miyabi/internal/pan"
	"github.com/ppxb/miyabi/internal/tasks"
)

func TestSubtitleFollowupRollsBackWithPublicationAndSurvivesRestart(t *testing.T) {
	ctx := t.Context()
	store, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	service := &Service{db: store.Client, subtitles: subtitleExporterFunc(func(context.Context, int, subtitlemeta.Target) (int, error) { return 0, nil })}
	cfg := export.Config{EmbyDir: t.TempDir()}
	input := MetadataPayload{MovieID: 7, Code: "ABP-123"}
	videos := []pan.File{{ID: "a", Name: "ABP-123.mp4"}}
	failure := errors.New("publication rollback")
	err = ent.WithTx(ctx, store.Client, func(tx *ent.Tx) error {
		if err := service.queueSubtitlesTx(ctx, tx, input, videos, cfg); err != nil {
			return err
		}
		return failure
	})
	if !errors.Is(err, failure) || store.Client.Task.Query().CountX(ctx) != 0 {
		t.Fatal("subtitle escaped rollback")
	}
	for range 2 {
		if err := ent.WithTx(ctx, store.Client, func(tx *ent.Tx) error { return service.queueSubtitlesTx(ctx, tx, input, videos, cfg) }); err != nil {
			t.Fatal(err)
		}
	}
	if store.Client.Task.Query().CountX(ctx) != 1 {
		t.Fatal("pending target was duplicated")
	}
	registry := tasks.NewRegistry()
	registry.Register(tasks.NewHandler(tasks.KindSubtitle, service.ExportSubtitle, nil))
	queue := tasks.NewService(store.Client, registry).Queue()
	job, err := queue.Claim(ctx, []tasks.Kind{tasks.KindSubtitle})
	if err != nil || job == nil {
		t.Fatalf("claim: %v", err)
	}
	// Recreate the queue as after a process restart. No in-memory channel is needed.
	queue = tasks.NewService(store.Client, registry).Queue()
	if err := queue.Recover(ctx, []tasks.Kind{tasks.KindSubtitle}); err != nil {
		t.Fatal(err)
	}
	replayed, err := queue.Claim(ctx, []tasks.Kind{tasks.KindSubtitle})
	if err != nil || replayed == nil || replayed.ID != job.ID {
		t.Fatalf("restart lost follow-up: %+v %v", replayed, err)
	}
	if err := service.ExportSubtitle(ctx, *replayed); err != nil {
		t.Fatal(err)
	}
	if err := queue.Finish(ctx, replayed.ID, nil); err != nil {
		t.Fatal(err)
	}
	if store.Client.Task.GetX(ctx, replayed.ID).Status != task.StatusDone {
		t.Fatal("follow-up not completed")
	}
}

func TestSubtitleFollowupsKeepVersionsSeparateAndSkipFragments(t *testing.T) {
	ctx := t.Context()
	store, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	service := &Service{db: store.Client, subtitles: subtitleExporterFunc(func(context.Context, int, subtitlemeta.Target) (int, error) { return 0, nil })}
	input := MetadataPayload{MovieID: 7, Code: "ABP-123"}
	videos := []pan.File{{ID: "a", Name: "ABP-123-1080p.mp4"}, {ID: "b", Name: "ABP-123-U.mp4"}}
	cfg := export.Config{EmbyDir: t.TempDir()}
	if err := ent.WithTx(ctx, store.Client, func(tx *ent.Tx) error { return service.queueSubtitlesTx(ctx, tx, input, videos, cfg) }); err != nil {
		t.Fatal(err)
	}
	rows := store.Client.Task.Query().AllX(ctx)
	if len(rows) != 2 || rows[0].ResourceKey == rows[1].ResourceKey {
		t.Fatal("distinct versions coalesced")
	}
	for _, row := range rows {
		p, err := tasks.DecodePayload[SubtitlePayload](row.Payload)
		if err != nil || p.Target.FileID == "" || p.Target.Dir == "" {
			t.Fatal("incomplete durable target")
		}
	}
	if err := ent.WithTx(ctx, store.Client, func(tx *ent.Tx) error {
		return service.queueSubtitlesTx(ctx, tx, input, []pan.File{{ID: "c", Name: "ABP-123-CD2.mp4"}}, cfg)
	}); err != nil {
		t.Fatal(err)
	}
	if store.Client.Task.Query().CountX(ctx) != 2 {
		t.Fatal("fragment queued whole-movie subtitle")
	}
}
