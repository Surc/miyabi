package catalogue

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ppxb/miyabi/internal/domain"
)

type browseFake struct {
	pages, details, magnets, images, ratings int
	partial, fail                            bool
	started, release                         chan struct{}
	detailStarted, detailRelease             chan struct{}
	tags                                     []string
	unrated                                  bool
}

func (f *browseFake) Browse(ctx context.Context, options domain.BrowseOptions) ([]Movie, error) {
	f.pages++
	if f.started != nil {
		close(f.started)
		<-f.release
	}
	if f.fail {
		return nil, errors.New("offline")
	}
	return []Movie{{Movie: domain.Movie{ID: "film", Code: "ABP-123", Cover: "https://example.com/cover.jpg", Rating: 4.2, RatingMax: 5, RatingSource: "javdb"}}}, nil
}
func (f *browseFake) Search(ctx context.Context, query string, options domain.SearchOptions) ([]Movie, error) {
	return f.Browse(ctx, domain.BrowseOptions{Page: options.Page})
}
func (f *browseFake) CatalogueDetail(ctx context.Context, id string) (domain.MovieDetail, error) {
	f.details++
	if f.detailStarted != nil {
		close(f.detailStarted)
		<-f.detailRelease
	}
	if f.fail {
		return domain.MovieDetail{}, errors.New("offline")
	}
	score := 4.2
	if f.unrated {
		score = 0
	}
	return domain.MovieDetail{Movie: domain.Movie{ID: id, Code: "ABP-123", Title: "Title", Cover: "https://example.com/cover.jpg", Rating: score, RatingMax: 5, HasSubtitle: true, MagnetsCount: 2}, Zone: domain.ZoneCensored}, nil
}
func (f *browseFake) BrowseResourceSnapshot(ctx context.Context, movie domain.MovieDetail) ([]Magnet, bool, error) {
	f.magnets++
	if f.fail {
		return nil, false, errors.New("offline")
	}
	tags := f.tags
	if tags == nil {
		tags = []string{domain.MagnetTagSubtitle, domain.MagnetTagCracked}
	}
	return []Magnet{{Magnet: domain.Magnet{Hash: "hash", Tags: tags}}}, !f.partial, nil
}
func (f *browseFake) Media(ctx context.Context, url string) (domain.Media, error) {
	f.images++
	return domain.Media{ContentType: "image/png", Body: []byte("image")}, nil
}
func (f *browseFake) JavLibraryRating(ctx context.Context, code string) (domain.ExternalRating, error) {
	f.ratings++
	score := 8.1
	return domain.ExternalRating{Source: "javlibrary", Score: &score, Max: 10, Found: true}, nil
}
func newBrowseTest(t *testing.T, f *browseFake) (*BrowseExtension, string) {
	t.Helper()
	directory := t.TempDir()
	s, err := NewBrowseExtension(directory, f, f)
	if err != nil {
		t.Fatal(err)
	}
	return s, directory
}
func TestBrowseCachePersistsResourcesAndOnlyLoadsImagesOnDemand(t *testing.T) {
	f := &browseFake{}
	s, directory := newBrowseTest(t, f)
	resource, err := s.Resources(t.Context(), "film", false)
	if err != nil || !resource.Data.HasSubtitle || !resource.Data.HasCracked || !resource.Data.Complete {
		t.Fatalf("%+v %v", resource, err)
	}
	if f.images != 0 {
		t.Fatal("preloading downloaded images")
	}
	if resource.Data.Movie.Rating != 0 || resource.Data.Movie.MagnetsCount != 0 || resource.Data.Movie.HasSubtitle {
		t.Fatal("dynamic fields persisted as permanent metadata")
	}
	if _, err = s.Image(t.Context(), "film"); err != nil {
		t.Fatal(err)
	}
	rating, err := s.Rating(t.Context(), "javlibrary", "film", false)
	if err != nil || rating.Data.Max != 10 || *rating.Data.Score != 8.1 {
		t.Fatalf("%+v %v", rating, err)
	}
	restarted, err := NewBrowseExtension(directory, f, f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.Resources(t.Context(), "film", false); err != nil {
		t.Fatal(err)
	}
	image, err := restarted.Image(t.Context(), "film")
	if err != nil || string(image.Body) != "image" {
		t.Fatalf("image=%+v %v", image, err)
	}
	if f.details != 1 || f.magnets != 1 || f.images != 1 {
		t.Fatalf("calls %+v", f)
	}
	stats, err := restarted.CacheStats()
	if err != nil || stats.Movies != 1 || stats.Images != 1 || stats.Bytes == 0 {
		t.Fatalf("stats %+v %v", stats, err)
	}
}
func TestBrowseCacheExpiresDynamicFieldsWithoutDeletingMetadata(t *testing.T) {
	f := &browseFake{}
	s, _ := newBrowseTest(t, f)
	now := time.Now()
	s.now = func() time.Time { return now }
	if _, err := s.Resources(t.Context(), "film", false); err != nil {
		t.Fatal(err)
	}
	now = now.Add(31 * time.Minute)
	s.loads.reset()
	old, err := s.Resources(t.Context(), "film", false)
	if err != nil || !old.Stale || f.magnets != 1 {
		t.Fatalf("old %+v %v", old, err)
	}
	f.fail = true
	failed, err := s.Resources(t.Context(), "film", true)
	if err != nil || !failed.Stale || !failed.RefreshFailed || !failed.Data.HasSubtitle {
		t.Fatalf("failed %+v %v", failed, err)
	}
	f.fail = false
	s.loads.reset()
	fresh, err := s.Resources(t.Context(), "film", true)
	if err != nil || fresh.Stale || f.details != 1 || f.magnets != 3 {
		t.Fatalf("fresh %+v calls %+v %v", fresh, f, err)
	}
	if _, err = s.Rating(t.Context(), "javdb", "film", false); err != nil {
		t.Fatal(err)
	}
	if f.details != 1 {
		t.Fatal("fresh persisted score fetched again")
	}
	now = now.Add(12 * time.Hour)
	s.loads.reset()
	rating, err := s.Rating(t.Context(), "javdb", "film", true)
	if err != nil || rating.Stale || rating.Data.Max != 5 || f.details != 2 {
		t.Fatalf("rating %+v calls %+v %v", rating, f, err)
	}
}
func TestPartialBrowseResourcesStayRetryableAndCrackedDoesNotMeanUncensored(t *testing.T) {
	f := &browseFake{partial: true}
	s, _ := newBrowseTest(t, f)
	for range 2 {
		r, err := s.Resources(t.Context(), "film", true)
		if err != nil || !r.Stale || r.Data.Complete {
			t.Fatalf("%+v %v", r, err)
		}
	}
	if f.magnets != 2 {
		t.Fatal("partial snapshot cached as complete")
	}
	f.partial = false
	r, err := s.Resources(t.Context(), "film", true)
	if err != nil || r.Stale {
		t.Fatalf("%+v %v", r, err)
	}
}
func TestBrowseCacheSettingsAndScopedClearPreserveLibraryData(t *testing.T) {
	f := &browseFake{}
	s, directory := newBrowseTest(t, f)
	library := filepath.Join(directory, "miyabi.db")
	if err := os.WriteFile(library, []byte("library"), 0600); err != nil {
		t.Fatal(err)
	}
	settings := DefaultBrowseCacheSettings()
	settings.Pages = 3
	if _, err := s.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	settings.Pages = 0
	if _, err := s.UpdateSettings(settings); !domain.IsKind(err, domain.KindInvalid) {
		t.Fatal("invalid settings accepted")
	}
	if _, err := s.Image(t.Context(), "film"); err != nil {
		t.Fatal(err)
	}
	stats, err := s.ClearCache("images")
	if err != nil || stats.Movies != 1 || stats.Images != 0 {
		t.Fatalf("%+v %v", stats, err)
	}
	if _, err = s.ClearCache("../"); !domain.IsKind(err, domain.KindInvalid) {
		t.Fatal("unsafe scope accepted")
	}
	if _, err = s.ClearCache("all"); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewBrowseExtension(directory, f, f)
	if err != nil || restarted.Settings().Pages != 3 {
		t.Fatalf("settings lost %v", err)
	}
	body, err := os.ReadFile(library)
	if err != nil || string(body) != "library" {
		t.Fatal("library touched")
	}
}
func TestClearRejectsInFlightIndexAndRatingWrites(t *testing.T) {
	f := &browseFake{started: make(chan struct{}), release: make(chan struct{})}
	s, _ := newBrowseTest(t, f)
	done := make(chan error, 1)
	go func() {
		_, err := s.BrowsePage(context.Background(), domain.BrowseOptions{Page: 1, Limit: 20}, false)
		done <- err
	}()
	<-f.started
	if _, err := s.ClearCache("all"); err != nil {
		t.Fatal(err)
	}
	close(f.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	for _, namespace := range []string{"indexes", "ratings"} {
		entries, _ := os.ReadDir(filepath.Join(s.store.directory, namespace))
		if len(entries) != 0 {
			t.Fatalf("old request resurrected %s", namespace)
		}
	}
}
func TestBrowseIndexesAreSeparatedByPageAndFilters(t *testing.T) {
	f := &browseFake{}
	s, _ := newBrowseTest(t, f)
	for _, options := range []domain.BrowseOptions{{Page: 1, Limit: 20}, {Page: 2, Limit: 20}, {Page: 1, Limit: 20, Main: []string{"c"}}} {
		if _, err := s.BrowsePage(t.Context(), options, false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.BrowsePage(t.Context(), domain.BrowseOptions{Page: 1, Limit: 20}, false); err != nil {
		t.Fatal(err)
	}
	if f.pages != 3 {
		t.Fatalf("page cache calls=%d", f.pages)
	}
	r, ok, _ := s.store.read("ratings", "javdb:film")
	var rating domain.ExternalRating
	if !ok || json.Unmarshal(r.Data, &rating) != nil || rating.Max != 5 || rating.Source != "javdb" {
		t.Fatal("source rating lost")
	}
}
func TestClearRejectsNestedImageWritesAfterMetadataReturns(t *testing.T) {
	f := &browseFake{detailStarted: make(chan struct{}), detailRelease: make(chan struct{})}
	s, _ := newBrowseTest(t, f)
	done := make(chan error, 1)
	go func() { _, err := s.Image(context.Background(), "film"); done <- err }()
	<-f.detailStarted
	if _, err := s.ClearCache("all"); err != nil {
		t.Fatal(err)
	}
	close(f.detailRelease)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	stats, err := s.CacheStats()
	if err != nil || stats.Bytes != 0 {
		t.Fatalf("cleared data resurrected: %+v %v", stats, err)
	}
}
func TestUncensoredResourceDoesNotCreateCrackedBadge(t *testing.T) {
	f := &browseFake{tags: []string{domain.MagnetTagUncensored}}
	s, _ := newBrowseTest(t, f)
	r, err := s.Resources(t.Context(), "film", false)
	if err != nil || r.Data.HasCracked || r.Data.HasSubtitle || !r.Data.Complete {
		t.Fatalf("resource labels=%+v %v", r, err)
	}
}
func TestUnratedAndCanceledCacheReadsRemainDistinct(t *testing.T) {
	f := &browseFake{unrated: true}
	s, _ := newBrowseTest(t, f)
	r, err := s.Rating(t.Context(), "javdb", "film", false)
	if err != nil || !r.Data.Found || r.Data.Score != nil {
		t.Fatalf("unrated=%+v %v", r, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Rating(ctx, "javdb", "film", false); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}
func TestCoverAndRatingUseListReferencesWithoutDetailFanout(t *testing.T) {
	f := &browseFake{}
	s, _ := newBrowseTest(t, f)
	if _, err := s.BrowsePage(t.Context(), domain.BrowseOptions{Page: 1, Limit: 20}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Image(t.Context(), "film"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rating(t.Context(), "javlibrary", "film", false); err != nil {
		t.Fatal(err)
	}
	if f.details != 0 || f.magnets != 0 || f.images != 1 || f.ratings != 1 {
		t.Fatalf("unexpected fanout %+v", f)
	}
}
