package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ncruces/go-sqlite3"
	"github.com/ncruces/go-sqlite3/driver"
	"github.com/ncruces/go-sqlite3/ext/fts5"
)

// FTS5 is an opt-in extension since go-sqlite3 v0.35; register it on every connection.
func init() { sqlite3.AutoExtension(fts5.Register) }

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

	// busy_timeout is set in the connection init hook, which the driver runs
	// before any statement: database/sql connects lazily, so a PRAGMA statement
	// here would itself be the one to fail BUSY.
	sqlDB, err := driver.Open(path, func(c *sqlite3.Conn) error {
		return c.BusyTimeout(busyTimeout)
	})
	if err != nil {
		return nil, fmt.Errorf("opening sqlite3: %w", err)
	}

	// Single writer per process.
	sqlDB.SetMaxOpenConns(1)

	if err := execBusyRetry(ctx, sqlDB, `PRAGMA journal_mode=WAL`, busyTimeout); err != nil {
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

// busyTimeout bounds how long SQLite waits for a competing process before
// returning SQLITE_BUSY. It also bounds the journal-mode retry window below.
// Setting it explicitly is deliberate: the driver's own 1-minute default is
// skipped whenever any "_pragma" is given in the connection string, so relying
// on it would drop the timeout to zero the day one is added for another reason.
const busyTimeout = 10 * time.Second

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
// documents, and index runs. Orphaned content blobs (not referenced by any
// remaining document) are also pruned.
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
func (db *DB) RenameCollectionData(ctx context.Context, oldName, newName string) error {
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

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM content WHERE hash NOT IN (
			SELECT DISTINCT content_hash FROM documents
		)`); err != nil {
		return fmt.Errorf("pruning orphaned content: %w", err)
	}

	return tx.Commit()
}
