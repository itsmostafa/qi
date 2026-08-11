package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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

	// Single writer per process, plus a bounded cross-process wait before any
	// lock-prone initialization. This must precede journal-mode setup and
	// migrations so simultaneous first starts wait instead of failing BUSY.
	sqlDB.SetMaxOpenConns(1)
	if _, err := sqlDB.ExecContext(ctx, `PRAGMA busy_timeout=10000`); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("setting SQLite busy timeout: %w", err)
	}

	if err := execBusyRetry(ctx, sqlDB, `PRAGMA journal_mode=WAL`, 10*time.Second); err != nil {
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

// journal_mode can return SQLITE_BUSY immediately even with busy_timeout, so
// retry lock failures within the same bounded initialization window.
func execBusyRetry(ctx context.Context, database *sql.DB, statement string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	delay := 5 * time.Millisecond
	for {
		_, err := database.ExecContext(ctx, statement)
		if err == nil {
			return nil
		}
		message := strings.ToLower(err.Error())
		if (!strings.Contains(message, "locked") && !strings.Contains(message, "busy")) || time.Now().After(deadline) {
			return err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if delay < 100*time.Millisecond {
			delay *= 2
		}
	}
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

// RenameCollectionData merges indexed data from oldName into newName.
func (db *DB) RenameCollectionData(ctx context.Context, oldName, newName, path string) error {
	if oldName == "" || oldName == newName {
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, table := range []string{"chunk_vectors", "embeddings"} {
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM `+table+` WHERE chunk_id IN (
				SELECT c.id FROM chunks c
				JOIN documents old ON old.id = c.doc_id
				JOIN documents new ON new.collection = ? AND new.path = old.path
				WHERE old.collection = ?
			)`, newName, oldName); err != nil {
			return fmt.Errorf("deleting duplicate %s: %w", table, err)
		}
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM chunks WHERE doc_id IN (
			SELECT old.id FROM documents old
			JOIN documents new ON new.collection = ? AND new.path = old.path
			WHERE old.collection = ?
		)`, newName, oldName); err != nil {
		return fmt.Errorf("deleting duplicate chunks: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM documents
		WHERE collection = ?
		  AND path IN (SELECT path FROM documents WHERE collection = ?)
	`, oldName, newName); err != nil {
		return fmt.Errorf("deleting duplicate documents: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE documents SET collection = ? WHERE collection = ?`, newName, oldName); err != nil {
		return fmt.Errorf("renaming documents: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE index_runs SET collection = ? WHERE collection = ?`, newName, oldName); err != nil {
		return fmt.Errorf("renaming index_runs: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM collections WHERE name = ?`, oldName); err != nil {
		return fmt.Errorf("deleting old collection row: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO collections(name, path, updated_at)
		VALUES (?, ?, datetime('now'))
		ON CONFLICT(name) DO UPDATE SET path = excluded.path, updated_at = datetime('now')
	`, newName, path); err != nil {
		return fmt.Errorf("upserting collection row: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM content WHERE hash NOT IN (
			SELECT DISTINCT content_hash FROM documents
		)`); err != nil {
		return fmt.Errorf("pruning orphaned content: %w", err)
	}

	return tx.Commit()
}
