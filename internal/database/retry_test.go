package database

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ppxb/miyabi/internal/domain"
)

func TestLibraryRetryRecognizesSQLiteWriterContention(t *testing.T) {
	ctx := t.Context()
	store, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	tx, err := store.Client.Tx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	other, err := store.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, err := other.ExecContext(ctx, "PRAGMA busy_timeout=1"); err != nil {
		t.Fatal(err)
	}
	_, err = other.ExecContext(ctx, "BEGIN IMMEDIATE")
	if err == nil {
		t.Fatal("second writer unexpectedly acquired the transaction")
	}
	if delay, retry := RetryDelay(fmt.Errorf("ingest local batch: %w", err)); !retry || delay != 0 {
		t.Fatalf("writer contention cannot resume: delay=%v retry=%v error=%v", delay, retry, err)
	}
}

func TestLibraryRetryPreservesUpstreamAndPermanentFailures(t *testing.T) {
	for _, scenario := range []struct {
		err   error
		wait  time.Duration
		retry bool
	}{
		{&domain.HTTPError{Source: "115", StatusCode: 429, RetryAfter: time.Minute}, time.Minute, true},
		{context.DeadlineExceeded, 0, true},
		{domain.E(domain.KindUnauthorized, "authorization required", nil), 0, false},
		{errors.New("database is locked"), 0, false},
		{nil, 0, false},
	} {
		if wait, retry := RetryDelay(scenario.err); wait != scenario.wait || retry != scenario.retry {
			t.Fatalf("retry policy changed for %v: wait=%v retry=%v", scenario.err, wait, retry)
		}
	}
	store, err := Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	store.Client.Movie.Create().SetCode("ABP-123").ExecX(t.Context())
	err = store.Client.Movie.Create().SetCode("ABP-123").Exec(t.Context())
	if err == nil {
		t.Fatal("duplicate movie did not violate the unique constraint")
	}
	if _, retry := RetryDelay(err); retry {
		t.Fatalf("permanent constraint failure was retried: %v", err)
	}
}
