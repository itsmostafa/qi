package db

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMigration004RecoversAfterColumnAddedWithoutVersion(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "qi.db")
	database, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	// Early-build state: the ALTER TABLE committed but the marker never was.
	if _, err := database.ExecContext(ctx, `DELETE FROM schema_version WHERE version = 4`); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = Open(ctx, path)
	if err != nil {
		t.Fatalf("reopening partially applied migration: %v", err)
	}
	defer database.Close()
	var versions, columns int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_version WHERE version = 4`).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('embeddings') WHERE name = 'fingerprint'`).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	if versions != 1 || columns != 1 {
		t.Fatalf("recovery left version=%d columns=%d, want 1/1", versions, columns)
	}
}

// Migration 006's DDL can be present without its schema_version marker on any
// database opened by a build that shipped the file before it recorded its own
// version. Re-running the ALTER TABLE would fail, so the guard must recognise
// the completed state and record the marker instead.
func TestMigration006RecoversAfterColumnsAddedWithoutVersion(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "qi.db")
	database, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `DELETE FROM schema_version WHERE version = 6`); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = Open(ctx, path)
	if err != nil {
		t.Fatalf("reopening partially applied migration: %v", err)
	}
	defer database.Close()

	var versions int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schema_version WHERE version = 6`).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if versions != 1 {
		t.Errorf("recovery left version 6 unrecorded")
	}
}
