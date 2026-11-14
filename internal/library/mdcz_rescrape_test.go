package library

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ppxb/miyabi/internal/catalogue"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/ent/movie"
	"github.com/ppxb/miyabi/internal/ent/task"
	"github.com/ppxb/miyabi/internal/export"
	"github.com/ppxb/miyabi/internal/library/scan"
	"github.com/ppxb/miyabi/internal/library/scrape"
	"github.com/ppxb/miyabi/internal/mdcz"
	"github.com/ppxb/miyabi/internal/metadata"
	"github.com/ppxb/miyabi/internal/metadata/providers"
	"github.com/ppxb/miyabi/internal/nfo"
	"github.com/ppxb/miyabi/internal/pan"
	"github.com/ppxb/miyabi/internal/tasks"
)

type ambiguousABF027Catalogue struct{ providers.JavDBCatalogue }

func (ambiguousABF027Catalogue) Search(context.Context, string, domain.SearchOptions) ([]catalogue.Movie, error) {
	return []catalogue.Movie{
		{Movie: domain.Movie{ID: "45yav", Code: "ABF-027", ReleaseDate: "2011-12-02"}},
		{Movie: domain.Movie{ID: "g0dk4G", Code: "ABF-027", ReleaseDate: "2023-09-14"}},
	}, nil
}

type selectedABF027Catalogue struct {
	ambiguousABF027Catalogue
	t *testing.T
}

func (s selectedABF027Catalogue) RefreshCatalogueDetail(_ context.Context, id string) (domain.MovieDetail, error) {
	if id != "g0dk4G" {
		s.t.Fatalf("unexpected catalogue lookup: %q", id)
	}
	return domain.MovieDetail{Movie: domain.Movie{
		ID: id, Code: "ABF-027", Title: "Selected title", ReleaseDate: "2023-09-14", Cover: "https://images.example/selected.jpg",
	}}, nil
}

func (selectedABF027Catalogue) Media(context.Context, string) (domain.Media, error) {
	var body bytes.Buffer
	err := jpeg.Encode(&body, image.NewRGBA(image.Rect(0, 0, 320, 480)), nil)
	return domain.Media{Body: body.Bytes(), ContentType: "image/jpeg"}, err
}

func TestRescrapePublishesTheSelectedSameNumberJavDBEntry(t *testing.T) {
	ctx := t.Context()
	lib, scanTask, scanInput := libraryFixture(t)
	video := fixtureVideo("101", "ABF-027.mp4")
	if err := indexScanPage(ctx, lib, scanTask.ID, "initial", "/Movies", []scan.Video{video}, &scanInput); err != nil {
		t.Fatal(err)
	}
	lib.database.Task.UpdateOneID(scanTask.ID).SetStatus(task.StatusDone).ExecX(ctx)
	record := lib.database.Movie.Query().OnlyX(ctx)
	record.Update().SetJavdbID("45yav").SetTitle("Old title").SetScrapeStatus(movie.ScrapeStatusDone).
		SetMetadata(&nfo.Movie{Code: record.Code, Title: "Old title", Plot: "Different movie"}).ExecX(ctx)

	client := stubOf(t, lib.drive)
	path := []pan.Directory{{ID: "10", Name: "Movies"}}
	client.list = func(context.Context, string, string, int, int) (pan.FilePage, error) {
		return pan.FilePage{Total: 1, Files: []pan.File{video.File}, Path: path}, nil
	}
	client.info = func(context.Context, string, string) (pan.FileInfo, error) {
		return pan.FileInfo{File: video.File, Path: path}, nil
	}
	native, err := metadata.New(ctx, lib.database, providers.NewJavDB(selectedABF027Catalogue{t: t}))
	if err != nil {
		t.Fatal(err)
	}
	defer native.Close()
	source, err := mdcz.New(ctx, lib.database, t.TempDir(), native)
	if err != nil {
		t.Fatal(err)
	}
	bridgeKey := strings.Repeat("x", 24)
	settings := mdcz.Settings{Enabled: true, ServerURL: "http://127.0.0.1:1", Position: "bottomLeft"}
	if err := source.UpdateExtensionSettings(ctx, mdcz.Update{Settings: settings, APIKey: &bridgeKey}); err != nil {
		t.Fatal(err)
	}
	exportConfig := export.Config{EmbyDir: t.TempDir(), PublicURL: "http://localhost"}
	worker := scrape.New(lib.database, lib.drive, source, lib.images, lib.tasks, scrape.Dependencies{
		ExportManager: export.NewManager(exportConfig), BeginAttempt: source.BeginAttempt, ArtworkDecorator: source,
	})
	workflow, err := lib.RescrapeMovie(ctx, record.ID, "", "g0dk4G")
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := lib.RescrapeMovie(ctx, record.ID, "", "g0dk4G")
	if err != nil || repeated.ID != workflow.ID {
		t.Fatalf("duplicate selection started another workflow: %+v, %v", repeated, err)
	}
	if _, err := lib.RescrapeMovie(ctx, record.ID, "", "45yav"); !domain.IsKind(err, domain.KindConflict) {
		t.Fatalf("concurrent selection: %v", err)
	}
	if before := lib.database.Movie.GetX(ctx, record.ID); domain.ValueOrZero(before.JavdbID) != "45yav" || before.Title != "Old title" {
		t.Fatal("selection changed published metadata before export")
	}
	job := lib.database.Task.Query().Where(task.TypeEQ(tasks.KindScrape.String())).OnlyX(ctx)
	if err := worker.Scrape(ctx, tasks.Job{ID: job.ID, Payload: job.Payload}); err != nil {
		t.Fatal(err)
	}
	saved := lib.database.Movie.GetX(ctx, record.ID)
	if domain.ValueOrZero(saved.JavdbID) != "g0dk4G" || saved.ScrapeStatus != movie.ScrapeStatusDone || saved.Metadata.Plot != "" || saved.Poster == nil {
		t.Fatal("selected metadata was lost or mixed with the previous entry")
	}
	dir := export.EmbyMovieDir(exportConfig.EmbyDir, record.Code)
	body, err := os.ReadFile(filepath.Join(dir, record.Code+".nfo"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := nfo.Decode(body)
	if err != nil || doc.JavDBID() != "g0dk4G" || doc.Title != "Selected title" {
		t.Fatalf("wrong NFO identity: %+v, %v", doc, err)
	}
}

func TestRescrapeABF027PublishesMDCzWithoutAmbiguousJavDB(t *testing.T) {
	const code = "ABF-027"
	imageID := strings.Repeat("a", 64)
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/metadata":
			_ = json.NewEncoder(w).Encode(struct {
				mdcz.Health
				domain.MovieMetadata
				Partial bool `json:"partial"`
			}{
				Health: mdcz.Health{Protocol: mdcz.Protocol, Revision: mdcz.EngineRevision},
				MovieMetadata: domain.MovieMetadata{
					Detail: domain.MovieDetail{Movie: domain.Movie{Code: code, Title: "MDCz title", Plot: "MDCz synopsis"}},
					Images: []domain.ImageCandidate{{Provider: "mdcz", Role: "cover", URL: "mdcz:v1/" + imageID}},
				},
			})
		case "/v1/images/" + imageID:
			_ = jpeg.Encode(w, image.NewRGBA(image.Rect(0, 0, 320, 480)), nil)
		default:
			http.NotFound(w, r)
		}
	}))
	defer bridge.Close()
	verifyABF027Rescrape(t, bridge.URL, strings.Repeat("x", 24))
}

func verifyABF027Rescrape(t *testing.T, bridgeURL, bridgeKey string) {
	t.Helper()
	const code = "ABF-027"
	ctx := t.Context()
	lib, scanTask, scanInput := libraryFixture(t)
	video := fixtureVideo("101", code+".mp4")
	if err := indexScanPage(ctx, lib, scanTask.ID, "initial", "/Movies", []scan.Video{video}, &scanInput); err != nil {
		t.Fatal(err)
	}
	lib.database.Task.UpdateOneID(scanTask.ID).SetStatus(task.StatusDone).ExecX(ctx)
	record := lib.database.Movie.Query().OnlyX(ctx)
	client := stubOf(t, lib.drive)
	path := []pan.Directory{{ID: "10", Name: "Movies"}}
	client.list = func(context.Context, string, string, int, int) (pan.FilePage, error) {
		return pan.FilePage{Total: 1, Files: []pan.File{video.File}, Path: path}, nil
	}
	client.info = func(context.Context, string, string) (pan.FileInfo, error) {
		return pan.FileInfo{File: video.File, Path: path}, nil
	}
	base, err := metadata.New(ctx, lib.database, providers.NewJavDB(ambiguousABF027Catalogue{}))
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	source, err := mdcz.New(ctx, lib.database, t.TempDir(), base)
	if err != nil {
		t.Fatal(err)
	}
	settings := mdcz.Settings{Enabled: true, ServerURL: bridgeURL, Position: "bottomLeft"}
	if err := source.UpdateExtensionSettings(ctx, mdcz.Update{Settings: settings, APIKey: &bridgeKey}); err != nil {
		t.Fatal(err)
	}
	exportConfig := export.Config{EmbyDir: t.TempDir(), PublicURL: "http://localhost"}
	worker := scrape.New(lib.database, lib.drive, source, lib.images, lib.tasks, scrape.Dependencies{ExportManager: export.NewManager(exportConfig)})
	defer worker.Close()
	workflow, err := lib.RescrapeMovie(ctx, record.ID, "", "")
	if err != nil || workflow.MovieID != record.ID || workflow.Scan.MetadataTotal != 1 {
		t.Fatalf("single-movie workflow: %+v, %v", workflow, err)
	}
	job := lib.database.Task.Query().Where(task.TypeEQ(tasks.KindScrape.String())).OnlyX(ctx)
	if err := worker.Scrape(ctx, tasks.Job{ID: job.ID, Payload: job.Payload}); err != nil {
		t.Fatal(err)
	}
	saved := lib.database.Movie.GetX(ctx, record.ID)
	if saved.ScrapeStatus != movie.ScrapeStatusDone || saved.Code != code || saved.Title == "" || saved.Poster == nil || saved.Cover == nil || saved.Metadata == nil || saved.Metadata.Plot == "" {
		t.Fatal("MDCz metadata and artwork were not published")
	}
	if saved.JavdbID != nil || saved.Metadata.JavDBID() != "" {
		t.Fatal("an ambiguous JavDB entry was associated with the movie")
	}
	body, err := os.ReadFile(filepath.Join(export.EmbyMovieDir(exportConfig.EmbyDir, code), code+".nfo"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := nfo.Decode(body)
	if err != nil || doc.Title != saved.Title || doc.JavDBID() != "" {
		t.Fatalf("invalid exported metadata: %v", err)
	}
}
