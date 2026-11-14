package subtitle

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	subtitlemeta "github.com/ppxb/miyabi/internal/domain/subtitle"
	"github.com/ppxb/miyabi/internal/ent/setting"
	"github.com/ppxb/miyabi/internal/export"
)

func TestSubtitleRecoversInstalledFileWithoutRedownload(t *testing.T) {
	service, db, movieID, target := exportFixture(t, map[string]string{"/one.srt": simplifiedSRT},
		Candidate{Name: "ABP-123.chs.srt", URL: "/one.srt", Format: "srt", Language: subtitlemeta.LangSimplifiedChinese, Version: subtitlemeta.VersionStandard})
	stopped := errors.New("simulated exit before subtitle registration")
	service.afterSubtitleInstall = func() error { return stopped }
	if _, err := service.Export(t.Context(), movieID, target); !errors.Is(err, stopped) {
		t.Fatalf("fault = %v", err)
	}
	if db.Subtitle.Query().CountX(t.Context()) != 0 || db.Setting.Query().Where(setting.KeyHasPrefix(pendingSubtitlePrefix)).CountX(t.Context()) != 1 {
		t.Fatal("handoff was not durable")
	}
	path := filepath.Join(target.Dir, "ABP-123.zh-CN.srt")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewService(db, nil) // Recovery has no finder/network dependency.
	cfg := DefaultConfig()
	cfg.Enabled = false
	if _, err := restarted.UpdateConfig(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := restarted.Recover(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("recovery changed installed bytes")
	}
	if db.Subtitle.Query().CountX(t.Context()) != 1 || db.Setting.Query().Where(setting.KeyHasPrefix(pendingSubtitlePrefix)).CountX(t.Context()) != 0 {
		t.Fatal("registration was not exactly once")
	}
}

func TestSubtitleRecoveryPreservesConflicts(t *testing.T) {
	for _, change := range []string{"content", "file-id", "symlink", "movie"} {
		t.Run(change, func(t *testing.T) {
			service, db, movieID, target := exportFixture(t, map[string]string{"/one.srt": simplifiedSRT}, Candidate{Name: "ABP-123.chs.srt", URL: "/one.srt", Format: "srt", Language: subtitlemeta.LangSimplifiedChinese, Version: subtitlemeta.VersionStandard})
			root := t.TempDir()
			target.Dir = export.EmbyMovieDir(root, target.Code)
			target.FileID = "video-one"
			if err := os.MkdirAll(target.Dir, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(target.Dir, target.Stem+".strm"), export.STRMContent("http://media", "video-one", ""), 0644); err != nil {
				t.Fatal(err)
			}
			service.SetExportManager(export.NewManager(export.Config{EmbyDir: root}))
			service.afterSubtitleInstall = func() error { return errors.New("stop") }
			if _, err := service.Export(t.Context(), movieID, target); err == nil {
				t.Fatal("missing fault")
			}
			if db.Setting.Query().Where(setting.KeyHasPrefix(pendingSubtitlePrefix)).CountX(t.Context()) != 1 {
				t.Fatal("missing pending record")
			}
			path := filepath.Join(target.Dir, "ABP-123.zh-CN.srt")
			switch change {
			case "content":
				if err := os.WriteFile(path, []byte("user edit"), 0644); err != nil {
					t.Fatal(err)
				}
			case "file-id":
				if err := os.WriteFile(filepath.Join(target.Dir, target.Stem+".strm"), export.STRMContent("http://media", "video-two", ""), 0644); err != nil {
					t.Fatal(err)
				}
			case "movie":
				db.Movie.UpdateOneID(movieID).SetCode("NEW-001").SaveX(t.Context())
			case "symlink":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				external := filepath.Join(t.TempDir(), "user.srt")
				if err := os.WriteFile(external, []byte("user edit"), 0644); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(external, path); err != nil {
					t.Fatal(err)
				}
			}
			service.afterSubtitleInstall = nil
			if err := service.Recover(t.Context()); err == nil {
				t.Fatal("unsafe recovery accepted")
			}
			if db.Subtitle.Query().CountX(t.Context()) != 0 || db.Setting.Query().Where(setting.KeyHasPrefix(pendingSubtitlePrefix)).CountX(t.Context()) != 1 {
				t.Fatal("conflict evidence lost")
			}
			if change == "content" || change == "symlink" {
				body, err := os.ReadFile(path)
				if err != nil || string(body) != "user edit" {
					t.Fatal("user data changed")
				}
			}
		})
	}
}

func TestSubtitleCancellationKeepsDurableIntent(t *testing.T) {
	service, db, movieID, target := exportFixture(t, map[string]string{"/one.srt": simplifiedSRT}, Candidate{Name: "ABP-123.chs.srt", URL: "/one.srt", Format: "srt", Language: subtitlemeta.LangSimplifiedChinese, Version: subtitlemeta.VersionStandard})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	service.afterSubtitleInstall = func() error { cancel(); return nil }
	if _, err := service.Export(ctx, movieID, target); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel = %v", err)
	}
	if db.Subtitle.Query().CountX(t.Context()) != 0 {
		t.Fatal("canceled registration committed")
	}
	service.afterSubtitleInstall = nil
	if err := service.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if db.Subtitle.Query().CountX(t.Context()) != 1 {
		t.Fatal("canceled handoff was lost")
	}
}

func TestSubtitleInitialBindingAndUnresolvedHandoffBlocksOtherWriters(t *testing.T) {
	service, db, movieID, target := exportFixture(t, map[string]string{"/one.srt": simplifiedSRT}, Candidate{Name: "ABP-123.chs.srt", URL: "/one.srt", Format: "srt", Language: subtitlemeta.LangSimplifiedChinese, Version: subtitlemeta.VersionStandard})
	root := t.TempDir()
	target.Dir = export.EmbyMovieDir(root, target.Code)
	target.FileID = "legacy-video"
	if err := os.MkdirAll(target.Dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target.Dir, target.Stem+".strm"), export.STRMContent("http://media", target.FileID, ""), 0644); err != nil {
		t.Fatal(err)
	}
	manager := export.NewManager(export.Config{EmbyDir: root})
	manager.EnablePublicationRecovery(t.TempDir(), func(*export.Publication) (bool, error) { t.Fatal("unexpected movie journal"); return false, nil })
	manager.SetBeforeWriteRecovery(func(cfg export.Config) error { return service.RecoverLocked(t.Context(), cfg) })
	service.SetExportManager(manager)
	service.afterSubtitleInstall = func() error { return errors.New("stop") }
	if _, err := service.Export(t.Context(), movieID, target); err == nil {
		t.Fatal("missing fault")
	}
	if _, err := os.Stat(filepath.Join(root, ".miyabi-publication-root")); err != nil {
		t.Fatalf("legacy root was not bound: %v", err)
	}
	if db.Setting.Query().Where(setting.KeyHasPrefix(pendingSubtitlePrefix)).CountX(t.Context()) != 1 {
		t.Fatal("legacy handoff missing")
	}
	path := filepath.Join(target.Dir, "ABP-123.zh-CN.srt")
	if err := os.WriteFile(path, []byte("external change"), 0644); err != nil {
		t.Fatal(err)
	}
	service.afterSubtitleInstall = nil
	called := false
	if err := manager.WithConfig(func(export.Config) error { called = true; return nil }); err == nil || called {
		t.Fatal("unresolved subtitle did not block another writer")
	}
	if !manager.PublicationStatus().Blocked {
		t.Fatal("write barrier is not visible")
	}
}

func TestSubtitleRecoveryResyncsInstalledFileBeforeRemovingIntent(t *testing.T) {
	service, db, movieID, target := exportFixture(t, map[string]string{"/one.srt": simplifiedSRT}, Candidate{Name: "ABP-123.chs.srt", URL: "/one.srt", Format: "srt", Language: subtitlemeta.LangSimplifiedChinese, Version: subtitlemeta.VersionStandard})
	diskFailure := errors.New("simulated directory sync failure after link")
	service.syncSubtitleFile = func(file *os.File) error {
		info, err := file.Stat()
		if err != nil {
			return err
		}
		if info.IsDir() {
			return diskFailure
		}
		return file.Sync()
	}
	if _, err := service.Export(t.Context(), movieID, target); !errors.Is(err, diskFailure) {
		t.Fatalf("initial sync failure = %v", err)
	}
	path := filepath.Join(target.Dir, "ABP-123.zh-CN.srt")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("link missing after sync failure: %v", err)
	}
	assertPending := func() {
		t.Helper()
		if db.Subtitle.Query().CountX(t.Context()) != 0 || db.Setting.Query().Where(setting.KeyHasPrefix(pendingSubtitlePrefix)).CountX(t.Context()) != 1 {
			t.Fatal("failed sync lost pending intent or registered file")
		}
	}
	assertPending()
	restarted := NewService(db, nil)
	for _, failDirectory := range []bool{false, true} {
		restarted.syncSubtitleFile = func(file *os.File) error {
			info, err := file.Stat()
			if err != nil {
				return err
			}
			if info.IsDir() == failDirectory {
				return diskFailure
			}
			return file.Sync()
		}
		if err := restarted.Recover(t.Context()); !errors.Is(err, diskFailure) {
			t.Fatalf("recovery sync failure = %v", err)
		}
		assertPending()
	}
	var files, dirs int
	restarted.syncSubtitleFile = func(file *os.File) error {
		info, err := file.Stat()
		if err != nil {
			return err
		}
		if info.IsDir() {
			dirs++
		} else {
			files++
		}
		return file.Sync()
	}
	if err := restarted.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if files != 1 || dirs != 1 {
		t.Fatalf("replay durability calls: files=%d dirs=%d", files, dirs)
	}
	if db.Subtitle.Query().CountX(t.Context()) != 1 || db.Setting.Query().Where(setting.KeyHasPrefix(pendingSubtitlePrefix)).CountX(t.Context()) != 0 {
		t.Fatal("successful durable replay did not settle intent")
	}
}
