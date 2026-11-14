package offline

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/domain/download"
	"github.com/ppxb/miyabi/internal/ent/offlinedownload"
	"github.com/ppxb/miyabi/internal/pan"
)

func TestED2KSubmissionRecoversByLinkAndCancelsByRemoteHash(t *testing.T) {
	service, client := offlineAddFixture(t)
	ctx := t.Context()
	const (
		movieID = "fixture-movie"
		hash    = "1234567890abcdef1234567890abcdef"
		id      = "ed2k:" + hash + ":1234"
		uri     = "ed2k://|file|ABP-123.mp4|1234|" + hash + "|/"
	)
	service.catalogue.(*stubCatalogue).ed2k = map[string][]domain.ED2KResource{
		movieID: {{ID: id, URI: uri}},
	}
	adds := 0
	var directoryID string
	client.addOffline = func(_ context.Context, _, link, directory string) (string, error) {
		adds++
		directoryID = directory
		if link != uri {
			t.Fatalf("submitted %q", link)
		}
		return "", context.DeadlineExceeded
	}
	client.offlineTasks = func(context.Context, string, int) (pan.OfflinePage, error) {
		return pan.OfflinePage{PageCount: 1, Tasks: []pan.OfflineTask{
			{Hash: offlineHashA, URL: "ed2k://|file|ABP-123.mp4|12345|" + hash + "|/", DirectoryID: directoryID},
			{Hash: offlineHashB, URL: "ed2k://|file|ABP-123%20HD.mp4|1234|" + strings.ToUpper(hash) + "|/", DirectoryID: directoryID},
		}}, nil
	}
	if _, err := service.AddED2K(ctx, movieID, "ed2k:"+hash+":12345", 1); !domain.IsKind(err, domain.KindInvalid) || adds != 0 {
		t.Fatalf("unlisted ED2K submitted: calls=%d error=%v", adds, err)
	}
	if _, err := service.AddED2K(ctx, movieID, id, 1); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	record := service.database.OfflineDownload.Query().OnlyX(ctx)
	if record.Hash != id || record.InfoHash != "" || record.Recovery.ED2KURI != uri {
		t.Fatalf("unexpected pending task: %+v", record)
	}
	service = New(service.database, service.catalogue, service.drive, service.tasks, service.library, service.submitTimeout)
	service.now = func() time.Time { return record.Recovery.RetryAt }
	if err := service.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	saved := service.database.OfflineDownload.GetX(ctx, record.ID)
	if adds != 1 || saved.InfoHash != offlineHashB || saved.Recovery.Action != "" {
		t.Fatalf("task did not recover: adds=%d record=%+v", adds, saved)
	}
	state, err := service.TryNext(ctx, record.ID)
	if err != nil || state.CanSwitch || state.Hash != id || !state.CanCancel {
		t.Fatalf("unexpected controls: %+v %v", state, err)
	}
	var removed string
	client.removeOffline = func(_ context.Context, _, taskHash string) error {
		removed = taskHash
		return nil
	}
	state, err = service.Cancel(ctx, record.ID)
	if err != nil || state.Status != string(offlinedownload.StatusCancelled) || removed != offlineHashB {
		t.Fatalf("cancellation used %q: %+v %v", removed, state, err)
	}
}

func TestED2KCompletionResolvesFileInReportedDirectory(t *testing.T) {
	service, record, _, source := offlineFixture(t)
	ctx := t.Context()
	const uri = "ed2k://|file|ABP-123.mp4|1234|1234567890abcdef1234567890abcdef|/"
	record = record.Update().SetRecovery(&download.Recovery{ED2KURI: uri}).SaveX(ctx)
	client := stubOf(t, service.drive)
	client.offlineTasks = func(context.Context, string, int) (pan.OfflinePage, error) {
		return pan.OfflinePage{PageCount: 1, Tasks: []pan.OfflineTask{
			{Hash: record.InfoHash, Status: 2, FileID: source.Directory.ID, DirectoryID: source.Directory.ID},
		}}, nil
	}
	client.info = func(_ context.Context, _, id string) (pan.FileInfo, error) {
		if id != source.Directory.ID {
			t.Fatalf("unexpected reported location %q", id)
		}
		return pan.FileInfo{File: pan.File{ID: id, IsDirectory: true}}, nil
	}
	files := []pan.File{
		{ID: "old-version", Name: "ABP-123.mp4", Size: 123},
		{ID: "same-size", Name: "different.mp4", Size: 1234},
	}
	client.list = func(_ context.Context, _, id string, _, _ int) (pan.FilePage, error) {
		if id != source.Directory.ID {
			t.Fatalf("unexpected search directory %q", id)
		}
		page := directoryPage(source.Directory)
		page.Files, page.Total = files, len(files)
		return page, nil
	}
	if err := service.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	waiting := service.database.OfflineDownload.GetX(ctx, record.ID)
	if waiting.Status != offlinedownload.StatusDone || !waiting.AwaitingLocation || waiting.ScanTaskID != 0 {
		t.Fatalf("scanned the reported directory before the file was present: %+v", waiting)
	}
	files = append(files, pan.File{ID: "ed2k-video", Name: "ABP-123.mp4", Size: 1234})
	if err := service.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	complete := service.database.OfflineDownload.GetX(ctx, record.ID)
	if complete.FileID != "ed2k-video" || complete.AwaitingLocation || complete.ScanTaskID == 0 {
		t.Fatalf("did not scan the matching file: %+v", complete)
	}
}
