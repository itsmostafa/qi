package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/ncruces/go-sqlite3/driver"
)

// DB wraps a *sql.DB with qi-specific helpers.
type DB struct {
	*sql.DB
}

// Open opens (or creates) the qi SQLite database at path, runs migrations,
// and configures WAL mode.
func Open(ctx context.Context, path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("creating db dir: %w", err)
	}

	sqlDB, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite3: %w", err)
	}

	// Single writer to avoid SQLITE_BUSY under WAL
	sqlDB.SetMaxOpenConns(1)

	if _, err := sqlDB.ExecContext(ctx, `PRAGMA journal_mode=WAL`); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("enabling WAL: %w", err)
	}

	if _, err := sqlDB.ExecContext(ctx, `PRAGMA foreign_keys=ON`); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("enabling foreign keys: %w", err)
	}

	db := &DB{sqlDB}

	if err := runMigrations(ctx, sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return db, nil
}

// Ping verifies the database connection.
func (db *DB) Ping(ctx context.Context) error {
	return db.PingContext(ctx)
}

// DeleteCollection removes all data associated with the given collection name:
// chunk vectors, embeddings, chunks (FTS triggers keep chunks_fts in sync),
// documents, index runs, and the collections table row. Orphaned content blobs
// (not referenced by any remaining document) are also pruned.
func (db *DB) DeleteCollection(ctx context.Context, name string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// chunk_vectors and embeddings for all chunks in this collection's documents
	for _, table := range []string{"chunk_vectors", "embeddings"} {
		_, err := tx.ExecContext(ctx, `
			DELETE FROM `+table+` WHERE chunk_id IN (
				SELECT c.id FROM chunks c
				JOIN documents d ON d.id = c.doc_id
				WHERE d.collection = ?
			)`, name)
		if err != nil {
			return fmt.Errorf("deleting %s: %w", table, err)
		}
	}

	// chunks (FTS triggers handle chunks_fts automatically)
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM chunks WHERE doc_id IN (
			SELECT id FROM documents WHERE collection = ?
		)`, name); err != nil {
		return fmt.Errorf("deleting chunks: %w", err)
	}

	// documents
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM documents WHERE collection = ?`, name); err != nil {
		return fmt.Errorf("deleting documents: %w", err)
	}

	// index run history
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM index_runs WHERE collection = ?`, name); err != nil {
		return fmt.Errorf("deleting index_runs: %w", err)
	}

	// collections table row
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM collections WHERE name = ?`, name); err != nil {
		return fmt.Errorf("deleting collection row: %w", err)
	}

	// orphaned content blobs (not referenced by any document)
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM content WHERE hash NOT IN (
			SELECT DISTINCT content_hash FROM documents
		)`); err != nil {
		return fmt.Errorf("pruning orphaned content: %w", err)
	}

	return tx.Commit()
}
