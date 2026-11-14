package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/task"
	"github.com/ppxb/miyabi/internal/export"
	"github.com/ppxb/miyabi/internal/library/scrape"
	"github.com/ppxb/miyabi/internal/nfo"
	"github.com/ppxb/miyabi/internal/pan"
	"github.com/ppxb/miyabi/internal/tasks"
)

type publicationMetadata struct {
	fixtureMetadata
	title string
}

func (p publicationMetadata) Resolve(ctx context.Context, ref domain.MovieRef) (domain.MovieMetadata, error) {
	value, err := p.fixtureMetadata.Resolve(ctx, ref)
	value.Detail.Title = p.title
	return value, err
}

func enableFixturePublication(t *testing.T, f *pipelineFixture) (*export.Manager, string) {
	t.Helper()
	manager := export.NewManager(export.Config{EmbyDir: f.embyDir, PublicURL: "http://127.0.0.1:8080"})
	directory := filepath.Join(f.dataDir, "export-publications")
	manager.EnablePublicationRecovery(directory, func(p *export.Publication) (bool, error) {
		return scrape.PublicationCommitted(t.Context(), f.store.Client, p)
	})
	f.scrape = scrape.New(f.store.Client, f.driveService, fixtureMetadata{f.discover}, f.images, f.tasks, scrape.Dependencies{ExportManager: manager})
	t.Cleanup(f.scrape.Close)
	f.tasks.Registry().Register(tasks.NewHandler(tasks.KindScrape, f.scrape.Scrape, f.scrape.Finished))
	return manager, directory
}

func TestPublicationDatabaseFailureRestoresSidecarsAndCanRetry(t *testing.T) {
	f := newPipelineFixture(t)
	ctx := t.Context()
	manager, journals := enableFixturePublication(t, f)
	f.drive.addFile("101", "10", "ABP-123.mp4", 2<<30, []byte("video"))
	f.addCatalogueMovie(fixtureDetail())
	if _, err := f.library.StartScan(ctx); err != nil {
		t.Fatal(err)
	}
	f.runQueue(t)
	before := f.store.Client.Movie.Query().OnlyX(ctx)
	path := filepath.Join(export.EmbyMovieDir(f.embyDir, before.Code), before.Code+".nfo")
	beforeNFO, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	detail := fixtureDetail()
	detail.Title = "New candidate title"
	f.scrape = scrape.New(f.store.Client, f.driveService, publicationMetadata{fixtureMetadata{f.discover}, detail.Title}, f.images, f.tasks, scrape.Dependencies{ExportManager: manager})
	t.Cleanup(f.scrape.Close)
	f.tasks.Registry().Register(tasks.NewHandler(tasks.KindScrape, f.scrape.Scrape, f.scrape.Finished))
	fail := true
	f.store.Client.Task.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			m := mutation.(*ent.TaskMutation)
			if body, ok := m.Payload(); ok && fail {
				p, err := tasks.DecodePayload[scrape.Payload](body)
				if err == nil && p.Completed && p.PublicationID != "" {
					return nil, errors.New("injected publication commit failure")
				}
			}
			return next.Mutate(ctx, mutation)
		})
	})
	if _, err := f.library.StartRebuild(ctx); err != nil {
		t.Fatal(err)
	}
	f.runQueue(t)
	after := f.store.Client.Movie.GetX(ctx, before.ID)
	afterNFO, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if before.Title != after.Title || !bytes.Equal(beforeNFO, afterNFO) {
		t.Fatal("rollback did not preserve published version")
	}
	pending, err := export.DiscoverPublications(journals)
	if err != nil || len(pending) != 0 {
		t.Fatalf("rollback left a journal: %v %v", pending, err)
	}
	job := f.store.Client.Task.Query().Where(task.TypeEQ(string(tasks.KindScrape)), task.StatusEQ(task.StatusFailed)).OnlyX(ctx)
	fail = false
	job.Update().SetStatus(task.StatusQueued).ExecX(ctx)
	f.runQueue(t)
	if row := f.store.Client.Task.GetX(ctx, job.ID); row.Status != task.StatusDone {
		t.Fatalf("retry did not complete: %+v", row)
	}
	if got := f.store.Client.Movie.GetX(ctx, before.ID).Title; got != detail.Title {
		t.Fatalf("retry published %q", got)
	}
}

func TestPublicationReceiptWinsOverPostCommitError(t *testing.T) {
	f := newPipelineFixture(t)
	ctx := t.Context()
	_, journals := enableFixturePublication(t, f)
	f.drive.addFile("101", "10", "ABP-123.mp4", 2<<30, []byte("video"))
	f.addCatalogueMovie(fixtureDetail())
	injected := false
	f.store.Client.Task.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			m := mutation.(*ent.TaskMutation)
			if body, ok := m.Payload(); ok {
				p, err := tasks.DecodePayload[scrape.Payload](body)
				if err == nil && p.Completed && p.PublicationID != "" {
					tx, err := m.Tx()
					if err != nil {
						return nil, err
					}
					tx.OnCommit(func(next ent.Committer) ent.Committer {
						return ent.CommitFunc(func(ctx context.Context, tx *ent.Tx) error {
							if err := next.Commit(ctx, tx); err != nil {
								return err
							}
							injected = true
							return errors.New("injected uncertain commit response")
						})
					})
				}
			}
			return next.Mutate(ctx, mutation)
		})
	})
	if _, err := f.library.StartScan(ctx); err != nil {
		t.Fatal(err)
	}
	f.runQueue(t)
	job := f.store.Client.Task.Query().Where(task.TypeEQ(string(tasks.KindScrape))).OnlyX(ctx)
	if !injected || job.Status != task.StatusDone {
		t.Fatalf("committed receipt lost: injected=%t row=%+v", injected, job)
	}
	pending, err := export.DiscoverPublications(journals)
	if err != nil || len(pending) != 0 {
		t.Fatalf("committed journal not finalized: %v %v", pending, err)
	}
	if _, err := os.Stat(filepath.Join(export.EmbyMovieDir(f.embyDir, "ABP-123"), "ABP-123.strm")); err != nil {
		t.Fatal(err)
	}
}

func TestPublicationStartupBarrierRecoversBeforeWriterAndBlocksUnknown(t *testing.T) {
	for _, missingReceipt := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "unknown"}[missingReceipt], func(t *testing.T) {
			f := newPipelineFixture(t)
			ctx := t.Context()
			manager, journals := enableFixturePublication(t, f)
			body, err := tasks.EncodePayload(scrape.Payload{MetadataPayload: scrape.MetadataPayload{MovieID: 7, Code: "ABP-123"}})
			if err != nil {
				t.Fatal(err)
			}
			job := f.store.Client.Task.Create().SetType(string(tasks.KindScrape)).SetPayload(body).SaveX(ctx)
			var publication *export.Publication
			err = manager.WithConfig(func(cfg export.Config) error {
				var err error
				publication, err = export.PreparePublication(journals, cfg, job.ID, 7, "ABP-123", "", nfo.Movie{Code: "ABP-123", Title: "candidate"}, []pan.File{{ID: "101", Name: "ABP-123.mp4"}}, nil, nil)
				if err != nil {
					return err
				}
				return publication.Install() // Simulated exit before SQL commit.
			})
			if err != nil {
				t.Fatal(err)
			}
			if missingReceipt {
				f.store.Client.Task.DeleteOneID(job.ID).ExecX(ctx)
			}
			fresh := export.NewManager(manager.Config())
			fresh.EnablePublicationRecovery(journals, func(p *export.Publication) (bool, error) { return scrape.PublicationCommitted(ctx, f.store.Client, p) })
			called := false
			err = fresh.WithConfig(func(export.Config) error { called = true; return nil })
			if missingReceipt {
				if status := fresh.PublicationStatus(); !status.Blocked || status.Pending != 1 {
					t.Fatalf("unsafe recovery snapshot: %+v", status)
				}
				if err == nil || called {
					t.Fatal("unknown commit allowed a writer")
				}
				if pending, err := export.DiscoverPublications(journals); err != nil || len(pending) != 1 {
					t.Fatal("unknown operation was discarded")
				}
			} else {
				if err != nil || !called {
					t.Fatalf("known rollback blocked: %v", err)
				}
				if status := fresh.PublicationStatus(); status.Blocked || status.Pending != 0 {
					t.Fatalf("recovered snapshot stale: %+v", status)
				}
				if _, err := os.Stat(filepath.Join(export.EmbyMovieDir(f.embyDir, "ABP-123"), "ABP-123.strm")); !os.IsNotExist(err) {
					t.Fatal("rollback retained candidate STRM")
				}
			}
		})
	}
}

func TestCommittedRebuildCleanupFailurePreservesMovieAndReplay(t *testing.T) {
	f := newPipelineFixture(t)
	ctx := t.Context()
	_, journals := enableFixturePublication(t, f)
	f.drive.addFile("101", "10", "ABP-123.mp4", 2<<30, []byte("video"))
	f.addCatalogueMovie(fixtureDetail())
	if _, err := f.library.StartScan(ctx); err != nil {
		t.Fatal(err)
	}
	f.runQueue(t)
	marker := filepath.Join(f.embyDir, ".miyabi-publication-root")
	held := marker + ".held"
	injected := false
	f.store.Client.Task.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			m := mutation.(*ent.TaskMutation)
			if body, ok := m.Payload(); ok && !injected {
				p, err := tasks.DecodePayload[scrape.Payload](body)
				if err == nil && p.Completed && p.Rebuild && p.PublicationID != "" {
					tx, err := m.Tx()
					if err != nil {
						return nil, err
					}
					tx.OnCommit(func(next ent.Committer) ent.Committer {
						return ent.CommitFunc(func(ctx context.Context, tx *ent.Tx) error {
							if err := next.Commit(ctx, tx); err != nil {
								return err
							}
							injected = true
							return os.Rename(marker, held) // Commit succeeds; journal finalization cannot verify the root.
						})
					})
				}
			}
			return next.Mutate(ctx, mutation)
		})
	})
	if _, err := f.library.StartRebuild(ctx); err != nil {
		t.Fatal(err)
	}
	f.runQueue(t)
	job := f.store.Client.Task.Query().Where(task.TypeEQ(string(tasks.KindScrape)), task.StatusEQ(task.StatusFailed)).OnlyX(ctx)
	payload, err := tasks.DecodePayload[scrape.Payload](job.Payload)
	if err != nil || !injected || !payload.Completed {
		t.Fatalf("expected committed failed finalization: %+v %v", payload, err)
	}
	if got := f.store.Client.Movie.Query().OnlyX(ctx).ScrapeStatus; string(got) != "done" {
		t.Fatalf("committed rebuild marked %s", got)
	}
	// Even a caller holding an old pre-publication job cannot downgrade it.
	stale, err := tasks.EncodePayload(scrape.Payload{MetadataPayload: payload.MetadataPayload})
	if err != nil {
		t.Fatal(err)
	}
	if err := ent.WithTx(ctx, f.store.Client, func(tx *ent.Tx) error {
		_, err := f.scrape.Finished(ctx, tx, tasks.Job{ID: job.ID, Payload: stale}, errors.New("stale completion"))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if got := f.store.Client.Movie.Query().OnlyX(ctx).ScrapeStatus; string(got) != "done" {
		t.Fatalf("stale callback downgraded movie: %s", got)
	}
	if err := os.Rename(held, marker); err != nil {
		t.Fatal(err)
	}
	job.Update().SetStatus(task.StatusQueued).ExecX(ctx)
	f.runQueue(t)
	if got := f.store.Client.Task.GetX(ctx, job.ID).Status; got != task.StatusDone {
		t.Fatalf("replay status %s", got)
	}
	if got := f.store.Client.Movie.Query().OnlyX(ctx).ScrapeStatus; string(got) != "done" {
		t.Fatalf("replay movie status %s", got)
	}
	if pending, err := export.DiscoverPublications(journals); err != nil || len(pending) != 0 {
		t.Fatalf("replay did not finalize: %v %v", pending, err)
	}
}
