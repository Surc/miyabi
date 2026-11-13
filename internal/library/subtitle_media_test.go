package library

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ppxb/miyabi/internal/export"
	"github.com/ppxb/miyabi/internal/nfo"
	"github.com/ppxb/miyabi/internal/pan"
)

func TestSubtitleVideosMatchMountedSTRMsByFileID(t *testing.T) {
	lib, _, payload := libraryFixture(t)
	ctx := t.Context()
	const code = "ABP-123"
	cfg := export.Config{EmbyDir: t.TempDir(), PublicURL: "http://localhost"}
	lib.exportMgr = export.NewManager(cfg)
	film := lib.database.Movie.Create().SetCode(code).SaveX(ctx)
	files := []pan.File{
		{ID: "normal", Name: "ABP-123.mp4"},
		{ID: "part1", Name: "ABP-123-CD1.mp4"},
		{ID: "part2", Name: "ABP-123-CD2.mp4"},
		{ID: "hidden", Name: "ABP-123-4K.mp4"},
	}
	for _, file := range files {
		root := payload.Source.Directory.ID
		if file.ID == "hidden" {
			root = "previous-mount"
		}
		lib.database.File.Create().SetFileID(file.ID).SetName(file.Name).SetSize(1).
			SetAccountID(payload.Source.AccountID).SetRootID(root).SetMovieID(film.ID).SaveX(ctx)
	}
	if _, err := export.WriteMedia(cfg, code, nfo.Movie{Code: code}, files, nil, nil); err != nil {
		t.Fatal(err)
	}
	plan, err := export.PlanVideos(code, files)
	if err != nil {
		t.Fatal(err)
	}
	for _, video := range plan {
		if video.File.ID == "part1" {
			path := filepath.Join(export.EmbyMovieDir(cfg.EmbyDir, code), video.Dir, video.Stem+".zh-CN.srt")
			if err := os.WriteFile(path, []byte("subtitle"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	videos, err := lib.MovieSubtitles(ctx, film.ID)
	if err != nil || len(videos) != 3 {
		t.Fatalf("visible videos = %+v, %v", videos, err)
	}
	for _, video := range videos {
		wantTracks := 0
		if video.Name == "ABP-123-CD1.mp4" {
			wantTracks = 1
		}
		if len(video.Tracks) != wantTracks {
			t.Fatalf("incorrect subtitle assignment: %+v", video)
		}
	}
}
