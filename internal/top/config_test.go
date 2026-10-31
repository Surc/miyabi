package top

import (
	"testing"

	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/domain"
)

func TestConfigPersistsAcrossClientsAndDatabaseRestart(t *testing.T) {
	ctx := t.Context()
	directory := t.TempDir()
	store, err := database.Open(ctx, directory)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store.Client)
	if got, err := service.Config(ctx); err != nil || got != DefaultConfig() {
		t.Fatalf("defaults = %+v, %v", got, err)
	}
	want := Config{ShowNavigation: false, ShowRankings: false, DefaultBoard: "2022"}
	if got, err := service.UpdateConfig(ctx, want); err != nil || got != want {
		t.Fatalf("save = %+v, %v", got, err)
	}
	if got, err := NewService(store.Client).Config(ctx); err != nil || got != want {
		t.Fatalf("another client = %+v, %v", got, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	restarted, err := database.Open(ctx, directory)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	service = NewService(restarted.Client)
	if got, err := service.Config(ctx); err != nil || got != want {
		t.Fatalf("after database restart = %+v, %v", got, err)
	}
	for _, board := range []string{"", "missing", "2026"} {
		invalid := want
		invalid.DefaultBoard = board
		if _, err := service.UpdateConfig(ctx, invalid); !domain.IsKind(err, domain.KindInvalid) {
			t.Fatalf("invalid board %q: %v", board, err)
		}
	}
	if got, err := service.Config(ctx); err != nil || got != want {
		t.Fatalf("invalid update changed persisted settings = %+v, %v", got, err)
	}
}
