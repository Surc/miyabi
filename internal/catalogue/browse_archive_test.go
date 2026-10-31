package catalogue

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ppxb/miyabi/internal/domain"
)

type archiveSource struct {
	browseFake
	tagCalls int
}

func (f *archiveSource) CatalogueDetail(ctx context.Context, id string) (domain.MovieDetail, error) {
	movie, err := f.browseFake.CatalogueDetail(ctx, id)
	movie.Actors = []domain.Actor{{ID: "actor", Name: "Actor", Avatar: "https://example.com/actor.jpg"}}
	movie.Tags = []domain.Tag{{ID: "tag", Name: "Tag"}}
	return movie, err
}

func (f *archiveSource) Tags(context.Context, domain.Zone) ([]domain.TagCategory, error) {
	f.tagCalls++
	return []domain.TagCategory{{ID: "category", Name: "Category", Tags: []domain.TagOption{{ID: "tag", Name: "Tag"}}}}, nil
}

func (f *archiveSource) JavLibraryRating(ctx context.Context, code string) (domain.ExternalRating, error) {
	if f.fail {
		f.ratings++
		return domain.ExternalRating{}, errors.New("offline")
	}
	return f.browseFake.JavLibraryRating(ctx, code)
}

func TestBrowseArchiveRestoresMetadataImagesRatingsAndTaxonomyAfterRestart(t *testing.T) {
	f := &archiveSource{}
	source, err := NewBrowseExtension(t.TempDir(), f, f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.BrowsePage(t.Context(), domain.BrowseOptions{Page: 1}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Resources(t.Context(), "film", false); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Image(t.Context(), "film"); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Media(t.Context(), "https://example.com/actor.jpg"); err != nil {
		t.Fatal(err)
	}
	rating, err := source.Rating(t.Context(), "javlibrary", "film", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Tags(t.Context(), domain.ZoneCensored, false); err != nil {
		t.Fatal(err)
	}
	settings := source.Settings()
	settings.Pages = 3
	if _, err := source.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := source.ExportCache(t.Context(), &archive); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	target, err := NewBrowseExtension(directory, f, f)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := source.CacheStats()
	stats, err := target.ImportCache(t.Context(), bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	if err != nil || stats != want {
		t.Fatalf("import=%+v want=%+v err=%v", stats, want, err)
	}
	restarted, err := NewBrowseExtension(directory, f, f)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.Settings() != DefaultBrowseCacheSettings() {
		t.Fatal("import overwrote server settings")
	}
	f.fail = true
	resource, err := restarted.Resources(t.Context(), "film", false)
	if err != nil || resource.Data.Movie.Actors[0].Name != "Actor" || resource.Data.Movie.Tags[0].Name != "Tag" || !resource.Data.HasSubtitle {
		t.Fatalf("restored metadata=%+v err=%v", resource, err)
	}
	image, err := restarted.Image(t.Context(), "film")
	if err != nil || string(image.Body) != "image" {
		t.Fatalf("cover=%+v err=%v", image, err)
	}
	if _, err := restarted.Media(t.Context(), "https://example.com/actor.jpg"); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Tags(t.Context(), domain.ZoneCensored, false); err != nil {
		t.Fatal(err)
	}
	byCode, err := restarted.RatingByCode(t.Context(), " abp-123 ", false)
	if err != nil || byCode.Data.Score == nil || *byCode.Data.Score != 8.1 || !byCode.UpdatedAt.Equal(rating.UpdatedAt) {
		t.Fatalf("rating=%+v err=%v", byCode, err)
	}
	if restarted.CachedRating("javlibrary", "film") == nil || restarted.CachedRating("javdb", "film") == nil {
		t.Fatal("restored rating missing from cache-only lookup")
	}
	if f.details != 1 || f.magnets != 1 || f.images != 2 || f.ratings != 1 || f.tagCalls != 1 {
		t.Fatalf("restored data contacted upstream: %+v", f)
	}
}

func TestPersistentRatingReturnsStaleScoreBeforeRefreshAndKeepsItOnFailure(t *testing.T) {
	f := &archiveSource{}
	s, err := NewBrowseExtension(t.TempDir(), f, f)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	s.now = func() time.Time { return now }
	if s.CachedRating("javlibrary-code", "ABP-123") != nil || f.ratings != 0 {
		t.Fatal("cache-only miss contacted upstream")
	}
	original, err := s.RatingByCode(t.Context(), "ABP-123", false)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(13 * time.Hour)
	stale := s.CachedRating("javlibrary-code", "ABP-123")
	if stale == nil || !stale.Stale || stale.Data.Score == nil || *stale.Data.Score != 8.1 || f.ratings != 1 {
		t.Fatalf("stale=%+v calls=%d", stale, f.ratings)
	}
	f.fail = true
	s.loads.reset()
	failed, err := s.RatingByCode(t.Context(), "ABP-123", true)
	if err != nil || !failed.RefreshFailed || !failed.Stale || !failed.UpdatedAt.Equal(original.UpdatedAt) || *failed.Data.Score != 8.1 {
		t.Fatalf("failure erased score: %+v %v", failed, err)
	}
	f.fail = false
	fresh, err := s.RatingByCode(t.Context(), "ABP-123", true)
	if err != nil || fresh.Stale || !fresh.UpdatedAt.After(original.UpdatedAt) {
		t.Fatalf("refresh=%+v err=%v", fresh, err)
	}
}

func TestBrowseArchiveMergeKeepsNewerLocalRecordsAndSettings(t *testing.T) {
	s, directory := newBrowseTest(t, &browseFake{})
	now := time.Now().UTC()
	key := browseRatingKey("javdb", "film")
	_, _, generation := s.store.read("ratings", key)
	local, _ := json.Marshal(domain.ExternalRating{Source: "javdb", Found: true, Max: 5})
	if err := s.store.write("ratings", key, browseRecord{now, local}, generation); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "miyabi.db"), []byte("library"), 0600); err != nil {
		t.Fatal(err)
	}
	settings := s.Settings()
	settings.Pages = 2
	if _, err := s.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	for _, age := range []time.Duration{-time.Hour, time.Hour} {
		data, _ := json.Marshal(browseRecord{now.Add(age), local})
		name := "ratings/" + filepath.Base(s.store.path("ratings", key))
		archive := makeBrowseArchive(t, [][2]string{{"manifest.json", `{"format":"miyabi-browse-cache","version":1}`}, {name, string(data)}})
		if _, err := s.ImportCache(t.Context(), bytes.NewReader(archive), int64(len(archive))); err != nil {
			t.Fatal(err)
		}
		cached := s.CachedRating("javdb", "film")
		want := now
		if age > 0 {
			want = now.Add(age)
		}
		if !cached.UpdatedAt.Equal(want) {
			t.Fatalf("timestamp=%s want=%s", cached.UpdatedAt, want)
		}
	}
	if s.Settings() != settings {
		t.Fatal("settings changed")
	}
	if body, err := os.ReadFile(filepath.Join(directory, "miyabi.db")); err != nil || string(body) != "library" {
		t.Fatal("library changed")
	}
}

func makeBrowseArchive(t *testing.T, entries [][2]string) []byte {
	t.Helper()
	var data bytes.Buffer
	w := zip.NewWriter(&data)
	for _, entry := range entries {
		file, err := w.Create(entry[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(entry[1])); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestInvalidBrowseArchivesLeaveExistingDataUntouched(t *testing.T) {
	s, _ := newBrowseTest(t, &browseFake{})
	if _, err := s.Resources(t.Context(), "film", false); err != nil {
		t.Fatal(err)
	}
	before, _ := s.CacheStats()
	valid := [2]string{"manifest.json", `{"format":"miyabi-browse-cache","version":1}`}
	name := "works/" + strings.Repeat("a", 64) + ".json"
	for _, entries := range [][][2]string{
		{{"manifest.json", `{"format":"other","version":1}`}},
		{{"manifest.json", `{"format":"miyabi-browse-cache","version":2}`}},
		{valid, valid},
		{valid, {"../outside.json", `{}`}},
		{valid, {"images/../../outside.json", `{}`}},
		{valid, {"config/" + strings.Repeat("a", 64) + ".json", `{}`}},
		{valid, {name, `{"updated_at":"2026-10-31T00:00:00Z","data":{"id":"film"}}`}, {"broken", `{}`}},
		{valid, {name, `{"updated_at":"2026-10-31T00:00:00Z","data":{}}`}},
		{valid, {name, `{"data":{}}`}},
		{{name, `{}`}},
	} {
		archive := makeBrowseArchive(t, entries)
		if _, err := s.ImportCache(t.Context(), bytes.NewReader(archive), int64(len(archive))); !domain.IsKind(err, domain.KindInvalid) {
			t.Fatalf("invalid archive accepted: %v", entries)
		}
		after, _ := s.CacheStats()
		if before != after {
			t.Fatal("invalid import modified existing data")
		}
	}
	if _, err := s.ImportCache(t.Context(), bytes.NewReader(nil), BrowseArchiveMaxBytes+1); !domain.IsKind(err, domain.KindInvalid) {
		t.Fatal("oversized upload accepted")
	}
}

func TestImportRejectsWritesFromEarlierRequests(t *testing.T) {
	f := &browseFake{started: make(chan struct{}), release: make(chan struct{})}
	s, _ := newBrowseTest(t, f)
	done := make(chan error, 1)
	go func() {
		_, err := s.BrowsePage(context.Background(), domain.BrowseOptions{Page: 1}, false)
		done <- err
	}()
	<-f.started
	archive := makeBrowseArchive(t, [][2]string{{"manifest.json", `{"format":"miyabi-browse-cache","version":1}`}})
	if _, err := s.ImportCache(t.Context(), bytes.NewReader(archive), int64(len(archive))); err != nil {
		t.Fatal(err)
	}
	close(f.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	stats, _ := s.CacheStats()
	if stats.Bytes != 0 {
		t.Fatalf("earlier request overwrote imported cache: %+v", stats)
	}
}
