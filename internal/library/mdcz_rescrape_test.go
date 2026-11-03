package library

import (
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
	workflow, err := lib.RescrapeMovie(ctx, record.ID, "")
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
