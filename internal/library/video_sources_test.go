package library

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/ppxb/miyabi/internal/domain/download"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/offlinedownload"
	"github.com/ppxb/miyabi/internal/pan"
)

func TestBackfillDownloadLinksRequiresEvidenceForEachVideo(t *testing.T) {
	magnet := func(c string) string { return "magnet:?xt=urn:btih:" + strings.Repeat(c, 40) }
	ed2k := "ed2k://|file|edition.mp4|123|" + strings.Repeat("a", 32) + "|/"
	videos := []*ent.File{
		{FileID: "501", ParentID: "10"},
		{FileID: "502", ParentID: "10"},
		{FileID: "503", ParentID: "20"},
		{FileID: "504", ParentID: "30"},
		{FileID: "505", ParentID: "40", Name: "edition.mp4", Size: 123},
		{FileID: "506", ParentID: "40", Name: "edition.mp4", Size: 456},
		{FileID: "507", ParentID: "10"},
	}
	known := []*ent.OfflineDownload{{
		Hash: strings.Repeat("a", 40), FileIds: []string{"501"},
		Recovery: &download.Recovery{CurrentHash: strings.Repeat("b", 40)},
	}}
	history := []pan.OfflineTask{
		{Status: 2, FileID: "502", URL: magnet("c")},
		{Status: 2, FileID: "20", DirectoryID: "10", URL: magnet("d")},
		{Status: 2, FileID: "30", DirectoryID: "10", URL: magnet("e")},
		{Status: 2, FileID: "30", DirectoryID: "10", URL: magnet("f")},
		{Status: 2, FileID: "40", DirectoryID: "40", URL: ed2k},
		{Status: 2, FileID: "10", DirectoryID: "10", URL: magnet("a")},
		{Status: -1, FileID: "507", URL: magnet("a")},
	}
	links := backfillDownloadLinks("10", videos, known, history)
	for id, uri := range map[string]string{"501": magnet("b"), "502": magnet("c"), "503": magnet("d"), "505": ed2k} {
		found := links[id]
		if len(found) != 1 || found[0].URI != uri || found[0].FromHistory != (id != "501") {
			t.Fatalf("incorrect owner for %s: %+v", id, found)
		}
	}
	for _, id := range []string{"504", "506", "507"} {
		if len(links[id]) != 0 {
			t.Fatalf("unconfirmed association for %s: %+v", id, links[id])
		}
	}
}

func TestDeleteMovieVideoKeepsOtherVersionsAndRetriesFailedRecycle(t *testing.T) {
	lib, _, payload := libraryFixture(t)
	ctx := t.Context()
	source := payload.Source
	client := stubOf(t, lib.drive)
	movie := lib.database.Movie.Create().SetCode("DEMO-001").SaveX(ctx)
	create := func(id, name string) *ent.File {
		return lib.database.File.Create().SetFileID(id).SetName(name).SetSize(123).
			SetMovieID(movie.ID).SetAccountID(source.AccountID).SetRootID(source.Directory.ID).
			SetParentID(source.Directory.ID).SaveX(ctx)
	}
	first := create("501", "DEMO-001-C.mp4")
	second := create("502", "DEMO-001-LTX.mp4")
	task := lib.database.OfflineDownload.Create().SetHash(strings.Repeat("a", 40)).
		SetAccountID(source.AccountID).SetDirectoryID(source.Directory.ID).SetStatus(offlinedownload.StatusDone).
		SetFileID(first.FileID).SetFileIds([]string{first.FileID, second.FileID}).SaveX(ctx)
	client.info = func(_ context.Context, _, id string) (pan.FileInfo, error) {
		return pan.FileInfo{
			File: pan.File{ID: id, ParentID: source.Directory.ID},
			Path: []pan.Directory{{ID: source.Directory.ID}},
		}, nil
	}
	unavailable := errors.New("115 temporarily unavailable")
	var recycled []string
	client.recycleFile = func(_ context.Context, _, id, parent string) error {
		if parent != source.Directory.ID {
			t.Fatalf("unexpected parent: %q", parent)
		}
		if unavailable != nil {
			return unavailable
		}
		recycled = append(recycled, id)
		return nil
	}
	if _, err := lib.DeleteMovieVideo(ctx, movie.ID, first.ID); !errors.Is(err, unavailable) {
		t.Fatalf("recycle error = %v", err)
	}
	if _, err := lib.database.File.Get(ctx, first.ID); err != nil {
		t.Fatalf("file index changed after failure: %v", err)
	}
	unavailable = nil
	if result, err := lib.DeleteMovieVideo(ctx, movie.ID, first.ID); err != nil || result.MovieRemoved {
		t.Fatalf("delete one version = %+v, %v", result, err)
	}
	task = lib.database.OfflineDownload.GetX(ctx, task.ID)
	if task.FileID != "" || !slices.Equal(task.FileIds, []string{second.FileID}) {
		t.Fatalf("incorrect remaining offline association: %+v", task)
	}
	if _, err := lib.database.File.Get(ctx, second.ID); err != nil {
		t.Fatalf("other version was removed: %v", err)
	}
	if result, err := lib.DeleteMovieVideo(ctx, movie.ID, second.ID); err != nil || !result.MovieRemoved {
		t.Fatalf("delete last version = %+v, %v", result, err)
	}
	_, err := lib.database.Movie.Get(ctx, movie.ID)
	if !slices.Equal(recycled, []string{first.FileID, second.FileID}) || !ent.IsNotFound(err) {
		t.Fatalf("unexpected final deletion: %v, %v", recycled, err)
	}
}
