package export

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ppxb/miyabi/internal/nfo"
	"github.com/ppxb/miyabi/internal/pan"
)

func journalFixture(t *testing.T) (Config, string, []pan.File, string) {
	t.Helper()
	base := t.TempDir()
	cfg := Config{EmbyDir: filepath.Join(base, "exports"), PublicURL: "http://localhost", STRMToken: "private-token"}
	files := []pan.File{{ID: "a", Name: "ABP-123.mp4"}}
	if _, err := WriteMedia(cfg, "ABP-123", nfo.Movie{Code: "ABP-123", Title: "old"}, files, []byte("old poster"), []byte("old fanart")); err != nil {
		t.Fatal(err)
	}
	subtitle := filepath.Join(EmbyMovieDir(cfg.EmbyDir, "ABP-123"), "ABP-123.zh-CN.srt")
	if err := os.WriteFile(subtitle, []byte("registered track"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(EmbyMovieDir(cfg.EmbyDir, "ABP-123"), "source-video.mp4"), []byte("untouched original"), 0644); err != nil {
		t.Fatal(err)
	}
	return cfg, filepath.Join(base, "data", "publications"), []pan.File{{ID: "a", Name: "ABP-123-1080p.mp4"}, {ID: "b", Name: "ABP-123-U.mp4"}}, subtitle
}
func journalSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() == publicationRootMarker {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result[rel] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func prepareFixture(t *testing.T, cfg Config, store string, videos []pan.File, subtitle string) *Publication {
	t.Helper()
	p, err := PreparePublication(store, cfg, 1, 1, "ABP-123", "", nfo.Movie{Code: "ABP-123", Title: "new"}, videos, []byte("new poster"), []byte("new fanart"), subtitle)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func TestPublicationRecoversEveryInterruptedOperation(t *testing.T) {
	// Discover through persisted bytes after each interruption; no process-local
	// progress flags or the original Publication pointer are needed.
	initialConfig, initialStore, initialVideos, initialSubtitle := journalFixture(t)
	operations := len(prepareFixture(t, initialConfig, initialStore, initialVideos, initialSubtitle).Entries)
	for _, committed := range []bool{false, true} {
		for stop := 0; stop < operations; stop++ {
			t.Run(fmt.Sprintf("committed=%t/operation=%d", committed, stop), func(t *testing.T) {
				cfg, store, videos, subtitle := journalFixture(t)
				before := journalSnapshot(t, cfg.EmbyDir)
				p := prepareFixture(t, cfg, store, videos, subtitle)
				if afterPrepare := journalSnapshot(t, cfg.EmbyDir); !reflect.DeepEqual(before, afterPrepare) {
					t.Fatal("preparation changed live media")
				}
				if within(cfg.EmbyDir, p.Stage) {
					t.Fatal("private backups are visible to media scan")
				}
				injected := errors.New("simulated termination")
				p.fault = func(index int) error {
					if index == stop {
						return injected
					}
					return nil
				}
				if err := p.Install(); !errors.Is(err, injected) || !errors.Is(err, ErrRecoveryRequired) {
					t.Fatalf("fault not injected: %v", err)
				}
				journals, err := DiscoverPublications(store)
				if err != nil || len(journals) != 1 {
					t.Fatalf("lost journal: %v", err)
				}
				if err := journals[0].Recover(committed); err != nil {
					t.Fatal(err)
				}
				if !committed {
					if got := journalSnapshot(t, cfg.EmbyDir); !reflect.DeepEqual(got, before) {
						t.Fatalf("rollback mismatch: %v", got)
					}
				} else {
					poster, err := os.ReadFile(filepath.Join(EmbyMovieDir(cfg.EmbyDir, "ABP-123"), "poster.jpg"))
					if err != nil || string(poster) != "new poster" {
						t.Fatal("committed candidate not retained")
					}
					if len(p.SubtitleMoves) != 1 {
						t.Fatalf("missing exact subtitle move: %v", p.SubtitleMoves)
					}
					for old, newPath := range p.SubtitleMoves {
						if _, err := os.Stat(old); !os.IsNotExist(err) {
							t.Fatal("old subtitle retained after committed move")
						}
						b, err := os.ReadFile(newPath)
						if err != nil || string(b) != "registered track" {
							t.Fatal("subtitle bytes lost")
						}
					}
				}
				journals, err = DiscoverPublications(store)
				if err != nil || len(journals) != 0 {
					t.Fatal("recovery not idempotently retired")
				}
				b, err := os.ReadFile(filepath.Join(EmbyMovieDir(cfg.EmbyDir, "ABP-123"), "source-video.mp4"))
				if err != nil || string(b) != "untouched original" {
					t.Fatal("source video changed")
				}
			})
		}
	}
}
func TestPublicationExternalChangesAndMissingEvidenceBlockRecovery(t *testing.T) {
	for _, damage := range []string{"target", "backup", "root", "digest", "symlink"} {
		t.Run(damage, func(t *testing.T) {
			cfg, store, videos, subtitle := journalFixture(t)
			p := prepareFixture(t, cfg, store, videos, subtitle)
			if err := p.Install(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(cfg.EmbyDir, p.Entries[0].Path)
			switch damage {
			case "target":
				if err := os.WriteFile(path, []byte("user edit"), 0600); err != nil {
					t.Fatal(err)
				}
			case "backup":
				for i, e := range p.Entries {
					if e.Before != "" {
						if err := os.Remove(p.blob(i, false)); err != nil {
							t.Fatal(err)
						}
						break
					}
				}
			case "root":
				if err := os.Remove(filepath.Join(cfg.EmbyDir, publicationRootMarker)); err != nil {
					t.Fatal(err)
				}
			case "digest":
				p.Digest = "0000000000000000000000000000000000000000000000000000000000000000"
			case "symlink":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(p.Stage, "working"), path); err != nil {
					t.Fatal(err)
				}
			}
			if err := p.Recover(false); !errors.Is(err, ErrRecoveryRequired) {
				t.Fatalf("unsafe recovery accepted: %v", err)
			}
			if _, err := os.Stat(p.catalog()); err != nil {
				t.Fatal("blocked journal removed")
			}
			if damage == "target" {
				b, _ := os.ReadFile(path)
				if string(b) != "user edit" {
					t.Fatal("external edit overwritten")
				}
			}
		})
	}
}
func TestPublicationUnregisteredSubtitleAndOldIdentityPreserved(t *testing.T) {
	cfg, store, videos, subtitle := journalFixture(t)
	p, err := PreparePublication(store, cfg, 1, 1, "ABP-124", "ABP-123", nfo.Movie{Code: "ABP-124", Title: "corrected"}, []pan.File{{ID: "a", Name: "ABP-124.mp4"}}, []byte("new"), []byte("new"), subtitle)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.SubtitleMoves) != 0 || len(p.SubtitleDeletes) != 0 {
		t.Fatal("old-code subtitle should remain until explicitly remapped")
	}
	if err := p.Install(); err != nil {
		t.Fatal(err)
	}
	if err := p.Recover(true); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(subtitle); err != nil || string(b) != "registered track" {
		t.Fatal("identity correction deleted sole subtitle")
	}
	// A conventionally named but unregistered subtitle is not moved or removed.
	cfg, store, videos, subtitle = journalFixture(t)
	p, err = PreparePublication(store, cfg, 2, 1, "ABP-123", "", nfo.Movie{Code: "ABP-123"}, videos, []byte("new"), []byte("new"))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Install(); err != nil {
		t.Fatal(err)
	}
	if err := p.Recover(true); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(subtitle); err != nil || string(b) != "registered track" {
		t.Fatal("unregistered subtitle modified")
	}
}
func TestPublicationRefusesSymlinkParentAndUnexpectedDestination(t *testing.T) {
	for _, kind := range []string{"parent", "collision", "catalog"} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			cfg := Config{EmbyDir: filepath.Join(base, "exports"), PublicURL: "http://localhost"}
			store := filepath.Join(base, "catalog")
			if err := os.MkdirAll(cfg.EmbyDir, 0755); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "parent":
				outside := filepath.Join(base, "outside")
				if err := os.Mkdir(outside, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(cfg.EmbyDir, "ABP")); err != nil {
					t.Fatal(err)
				}
			case "collision":
				dir := EmbyMovieDir(cfg.EmbyDir, "ABP-123")
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "poster.jpg"), []byte("user original"), 0644); err != nil {
					t.Fatal(err)
				}
			case "catalog":
				outside := filepath.Join(base, "outside")
				if err := os.Mkdir(outside, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, store); err != nil {
					t.Fatal(err)
				}
			}
			_, err := PreparePublication(store, cfg, 1, 1, "ABP-123", "", nfo.Movie{Code: "ABP-123"}, []pan.File{{ID: "a", Name: "ABP-123.mp4"}}, []byte("new"), nil)
			if err == nil {
				t.Fatal("unsafe prepare accepted")
			}
			if kind == "collision" {
				b, _ := os.ReadFile(filepath.Join(EmbyMovieDir(cfg.EmbyDir, "ABP-123"), "poster.jpg"))
				if !bytes.Equal(b, []byte("user original")) {
					t.Fatal("unowned original overwritten")
				}
			}
		})
	}
}

func TestPublicationRecoveryMayItselfBeInterrupted(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(fmt.Sprint(committed), func(t *testing.T) {
			cfg, store, videos, subtitle := journalFixture(t)
			before := journalSnapshot(t, cfg.EmbyDir)
			p := prepareFixture(t, cfg, store, videos, subtitle)
			stop := errors.New("stop")
			if committed {
				p.fault = func(int) error { return stop }
				if err := p.Install(); !errors.Is(err, stop) {
					t.Fatal(err)
				}
			} else {
				if err := p.Install(); err != nil {
					t.Fatal(err)
				}
			}
			journals, err := DiscoverPublications(store)
			if err != nil {
				t.Fatal(err)
			}
			journals[0].fault = func(int) error { return stop }
			if err := journals[0].Recover(committed); !errors.Is(err, stop) {
				t.Fatalf("recovery fault: %v", err)
			}
			journals, err = DiscoverPublications(store)
			if err != nil {
				t.Fatal(err)
			}
			if err := journals[0].Recover(committed); err != nil {
				t.Fatal(err)
			}
			if !committed && !reflect.DeepEqual(before, journalSnapshot(t, cfg.EmbyDir)) {
				t.Fatal("interrupted rollback lost original state")
			}
		})
	}
}

func TestPublicationNoopAndPrivateManifest(t *testing.T) {
	cfg, store, _, _ := journalFixture(t)
	p, err := PreparePublication(store, cfg, 1, 1, "ABP-123", "", nfo.Movie{Code: "ABP-123", Title: "old"}, []pan.File{{ID: "a", Name: "ABP-123.mp4"}}, []byte("old poster"), []byte("old fanart"))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Entries) != 0 {
		t.Fatal("unchanged output should have an empty write plan")
	}
	body, err := os.ReadFile(p.catalog())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte("private-token")) {
		t.Fatal("manifest exposed playback token")
	}
	for _, path := range []string{p.Stage, p.catalog()} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm()&0077 != 0 {
			t.Fatalf("private artifact permissions: %v %v", info, err)
		}
	}
	if err := p.Install(); err != nil {
		t.Fatal(err)
	}
	if err := p.Recover(true); err != nil {
		t.Fatal(err)
	}
}

func TestPublicationRootBindingSurvivesCompletedJournal(t *testing.T) {
	cfg, store, videos, subtitle := journalFixture(t)
	p := prepareFixture(t, cfg, store, videos, subtitle)
	if err := p.Recover(false); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(cfg.EmbyDir, publicationRootMarker)); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePublicationRoot(store, cfg.EmbyDir); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("unavailable bound root passed write barrier: %v", err)
	}
	before := journalSnapshot(t, cfg.EmbyDir)
	if _, err := PreparePublication(store, cfg, 2, 1, "ABP-123", "", nfo.Movie{Code: "ABP-123"}, videos, []byte("new"), nil); err == nil {
		t.Fatal("missing mount marker silently rebound")
	}
	if _, err := os.Stat(filepath.Join(cfg.EmbyDir, publicationRootMarker)); !os.IsNotExist(err) {
		t.Fatal("root marker was recreated despite existing binding")
	}
	if !reflect.DeepEqual(before, journalSnapshot(t, cfg.EmbyDir)) {
		t.Fatal("unavailable bound root received writes")
	}
}

func TestValidatePublicationRootWithoutPending(t *testing.T) {
	cfg, store, videos, subtitle := journalFixture(t)
	if err := ValidatePublicationRoot(store, cfg.EmbyDir); err != nil {
		t.Fatal(err)
	}
	p := prepareFixture(t, cfg, store, videos, subtitle)
	if err := p.Recover(false); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePublicationRoot(store, cfg.EmbyDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.EmbyDir, publicationRootMarker), []byte("00000000000000000000000000000000"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePublicationRoot(store, cfg.EmbyDir); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("changed root passed barrier: %v", err)
	}
}

func TestPublicationStageNeedsNoExportParentWritesAndRestoresWithStore(t *testing.T) {
	cfg, _, videos, subtitle := journalFixture(t)
	store := filepath.Join(t.TempDir(), "data", "publications")
	parent := filepath.Dir(cfg.EmbyDir)
	// A file at the old sibling stage location makes that layout unusable even
	// under root, where chmod alone cannot enforce a permission denial.
	oldStage := filepath.Join(parent, "."+filepath.Base(cfg.EmbyDir)+"-miyabi-publications")
	if err := os.WriteFile(oldStage, []byte("parent-owned sentinel"), 0400); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0755) })
	before := journalSnapshot(t, cfg.EmbyDir)
	p := prepareFixture(t, cfg, store, videos, subtitle)
	if p.Version != 2 || p.Stage != filepath.Join(store, "stages", p.ID) {
		t.Fatalf("stage outside backup store: %+v", p)
	}
	if err := p.Install(); err != nil {
		t.Fatal(err)
	}
	// Simulate a data-directory backup and restore, without separately copying
	// anything from the export root's parent. The restored catalog has all blobs.
	backup := filepath.Join(t.TempDir(), "backup")
	if err := os.CopyFS(backup, os.DirFS(store)); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(store); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backup, store); err != nil {
		t.Fatal(err)
	}
	pending, err := DiscoverPublications(store)
	if err != nil || len(pending) != 1 {
		t.Fatalf("restore discovery: %v %v", pending, err)
	}
	if err := pending[0].Recover(false); err != nil {
		t.Fatal(err)
	}
	if after := journalSnapshot(t, cfg.EmbyDir); !reflect.DeepEqual(before, after) {
		t.Fatal("restored store could not roll back")
	}
	if _, err := os.Stat(p.Stage); !os.IsNotExist(err) {
		t.Fatalf("stage not cleaned: %v", err)
	}
	if body, err := os.ReadFile(oldStage); err != nil || string(body) != "parent-owned sentinel" {
		t.Fatal("export parent changed")
	}
}

func TestPublicationRejectsLegacySiblingStage(t *testing.T) {
	cfg, store, videos, subtitle := journalFixture(t)
	p := prepareFixture(t, cfg, store, videos, subtitle)
	p.Version = 1
	p.Stage = filepath.Join(filepath.Dir(p.Root), "."+filepath.Base(p.Root)+"-miyabi-publications", p.ID)
	body, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.catalog(), body, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := DiscoverPublications(store); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("legacy discovery not blocked: %v", err)
	}
	if err := p.Install(); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("legacy journal not blocked: %v", err)
	}
	if _, err := os.Stat(p.catalog()); err != nil {
		t.Fatal("legacy catalog evidence removed")
	}
}
