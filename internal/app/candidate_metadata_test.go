package app

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ppxb/miyabi/internal/export"
	"github.com/ppxb/miyabi/internal/library/scrape"
	"github.com/ppxb/miyabi/internal/metadata"
	"github.com/ppxb/miyabi/internal/tasks"
)

func TestRebuildImageFailureKeepsPublishedMetadataAndIndexes(t *testing.T) {
	f := newPipelineFixture(t)
	ctx := t.Context()
	f.drive.addFile("101", "10", "ABP-123.mp4", 2<<30, []byte("video"))
	f.addCatalogueMovie(fixtureDetail())
	if _, err := f.library.StartScan(ctx); err != nil {
		t.Fatal(err)
	}
	f.runQueue(t)
	before := f.store.Client.Movie.Query().WithActors().WithTags().OnlyX(ctx)
	nfoPath := filepath.Join(export.EmbyMovieDir(f.embyDir, before.Code), before.Code+".nfo")
	beforeNFO, err := os.ReadFile(nfoPath)
	if err != nil {
		t.Fatal(err)
	}
	source := &workflowSource{id: "fixture", complete: true, failImage: true}
	meta, err := metadata.New(ctx, f.store.Client, source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(meta.Close)
	f.scrape = scrape.New(f.store.Client, f.driveService, meta, f.images, f.tasks, scrape.Dependencies{
		ExportManager: export.NewManager(export.Config{EmbyDir: f.embyDir, PublicURL: "http://127.0.0.1:8080"}),
	})
	t.Cleanup(f.scrape.Close)
	f.tasks.Registry().Register(tasks.NewHandler(tasks.KindScrape, f.scrape.Scrape, f.scrape.Finished))
	if _, err := f.library.StartRebuild(ctx); err != nil {
		t.Fatal(err)
	}
	f.runQueue(t)
	after := f.store.Client.Movie.Query().WithActors().WithTags().OnlyX(ctx)
	afterNFO, err := os.ReadFile(nfoPath)
	if err != nil {
		t.Fatal(err)
	}
	if before.Title != after.Title || !reflect.DeepEqual(before.Metadata, after.Metadata) ||
		!reflect.DeepEqual(before.Poster, after.Poster) || !bytes.Equal(beforeNFO, afterNFO) {
		t.Fatal("unpublished candidate replaced the previous metadata or files")
	}
	if len(before.Edges.Actors) != len(after.Edges.Actors) || len(before.Edges.Tags) != len(after.Edges.Tags) {
		t.Fatal("unpublished candidate replaced indexes")
	}
	for i := range before.Edges.Actors {
		if before.Edges.Actors[i].ID != after.Edges.Actors[i].ID {
			t.Fatal("actor index changed")
		}
	}
	for i := range before.Edges.Tags {
		if before.Edges.Tags[i].ID != after.Edges.Tags[i].ID {
			t.Fatal("tag index changed")
		}
	}
}
