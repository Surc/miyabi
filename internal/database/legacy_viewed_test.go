package database

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenPreservesLegacyViewedMovies(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", sqliteDSN(filepath.Join(dir, "miyabi.db")))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(t.Context(), `
		CREATE TABLE viewed_movies (id integer PRIMARY KEY, javdb_id text NOT NULL UNIQUE, viewed_at datetime NOT NULL);
		INSERT INTO viewed_movies VALUES (1, 'legacy-entry', CURRENT_TIMESTAMP), (2, 'another-entry', CURRENT_TIMESTAMP);
	`)
	closeErr := db.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("seed legacy database: %v, %v", err, closeErr)
	}
	store, err := Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	row, err := store.Client.ViewedMovie.Get(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if row.JavdbID != "legacy-entry" || row.Code != "" {
		t.Fatalf("legacy row = %#v", row)
	}
	if err := store.Client.ViewedMovie.Create().SetCode("NEW-001").Exec(t.Context()); err != nil {
		t.Fatalf("record a new view after migration: %v", err)
	}
	if count := store.Client.ViewedMovie.Query().CountX(t.Context()); count != 3 {
		t.Fatalf("view count = %d, want 3", count)
	}
}
