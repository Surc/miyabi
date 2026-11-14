package scrape

import (
	"context"
	"path/filepath"
	"testing"

	subtitlemeta "github.com/ppxb/miyabi/internal/domain/subtitle"
	"github.com/ppxb/miyabi/internal/export"
	"github.com/ppxb/miyabi/internal/pan"
)

type subtitleExporterFunc func(context.Context, int, subtitlemeta.Target) (int, error)

func (f subtitleExporterFunc) Export(ctx context.Context, movieID int, target subtitlemeta.Target) (int, error) {
	return f(ctx, movieID, target)
}

func TestSubtitleTaskTargetsTheExportedSTRM(t *testing.T) {
	service := New(nil, nil, nil, nil, nil, Dependencies{
		ExportManager: export.NewManager(export.Config{EmbyDir: "emby"}),
		Subtitles: subtitleExporterFunc(func(context.Context, int, subtitlemeta.Target) (int, error) {
			return 0, nil
		}),
	})
	t.Cleanup(service.Close)
	input := MetadataPayload{MovieID: 7, Code: "SSIS-589"}

	task := service.subtitleTask(input, []pan.File{{ID: "video", Name: "SSIS-589-UC.mp4"}})
	if task == nil || task.MovieID != 7 || len(task.Targets) != 1 || task.Targets[0].Dir != filepath.Join("emby", "SSIS", "SSIS-589") ||
		task.Targets[0].Stem != "SSIS-589" || task.Targets[0].Code != "SSIS-589" ||
		!task.Targets[0].Uncensored || !task.Targets[0].HardSubtitled {
		t.Fatalf("subtitle task = %+v", task)
	}
	if task := service.subtitleTask(input, []pan.File{{Name: "SSIS-589.mp4"}}); task == nil || task.Targets[0].Uncensored || task.Targets[0].HardSubtitled {
		t.Fatalf("plain release task = %+v", task)
	}
	parts := []pan.File{{Name: "SSIS-589-CD1.mp4"}, {Name: "SSIS-589-CD2.mp4"}}
	if task := service.subtitleTask(input, parts); task != nil {
		t.Fatalf("multi-part movie received a single subtitle target: %+v", task)
	}
	if task := service.subtitleTask(input, parts[1:]); task != nil {
		t.Fatal("remaining fragment received whole-movie subtitles")
	}
	versions := []pan.File{{ID: "original", Name: "SSIS-589-1080p.mp4"}, {ID: "uncensored", Name: "SSIS-589-U.mp4"}}
	task = service.subtitleTask(input, versions)
	plan, _ := export.PlanVideos(input.Code, versions)
	if task == nil || len(task.Targets) != 2 {
		t.Fatalf("version targets = %+v", task)
	}
	for i, target := range task.Targets {
		if target.Stem != plan[i].Stem || target.Version != subtitlemeta.DetectVersion(plan[i].File.Name) {
			t.Fatalf("version subtitle target differs from exported video: %+v", target)
		}
	}
	service.subtitles = nil
	if task := service.subtitleTask(input, []pan.File{{Name: "SSIS-589.mp4"}}); task != nil {
		t.Fatalf("subtitle task created without an exporter: %+v", task)
	}
}
