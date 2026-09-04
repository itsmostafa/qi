package db

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
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
//
// A document is a duplicate only when the destination holds the same relative
// path AND the same content hash — that is, genuinely the same file indexed
// under both names. Matching on path alone would delete real documents whenever
// the destination name happened to be occupied by a different directory that
// shares a filename, which basename-derived collection names make easy to hit.
// Documents that cannot move without overwriting a different file are left
// under oldName, where they stay searchable and can be removed deliberately.
func (db *DB) RenameCollectionData(ctx context.Context, oldName, newName string) error {
	return db.RenameCollections(ctx, [][2]string{{oldName, newName}})
}

// RenameCollections applies a whole set of {old, new} renames in one
// transaction. Renames are staged through temporary names first, because one
// collection's old name can be another's new name ("x-foo" -> "foo" alongside
// "y-x-foo" -> "x-foo"); renaming those in place would merge unrelated
// documents into whichever name happened to still be occupied.
func (db *DB) RenameCollections(ctx context.Context, renames [][2]string) error {
	var pending [][2]string
	for _, r := range renames {
		if r[0] != "" && r[0] != r[1] {
			pending = append(pending, r)
		}
	}
	if len(pending) == 0 {
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// A generated collection name never contains "/", so a staged name cannot
	// collide with a real one.
	staged := make([]string, len(pending))
	for i, r := range pending {
		staged[i] = fmt.Sprintf("/staging/%d", i)
		if _, err := renameCollectionTx(ctx, tx, r[0], staged[i]); err != nil {
			return err
		}
	}
	for i, r := range pending {
		stranded, err := renameCollectionTx(ctx, tx, staged[i], r[1])
		if err != nil {
			return err
		}
		if stranded == 0 {
			continue
		}
		// Documents that could not move without overwriting a different file
		// go back under their original name, where they stay searchable and
		// can be removed deliberately. That name is free: staging emptied it.
		slog.Warn("collection rename left documents behind: the new name is already used by different files",
			"old", r[0], "new", r[1], "documents", stranded)
		if _, err := renameCollectionTx(ctx, tx, staged[i], r[0]); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM content WHERE hash NOT IN (
			SELECT DISTINCT content_hash FROM documents
		)`); err != nil {
		return fmt.Errorf("pruning orphaned content: %w", err)
	}

	return tx.Commit()
}

// renameCollectionTx moves one collection's rows and reports how many
// documents stayed behind because newName already holds a different file at
// the same path.
func renameCollectionTx(ctx context.Context, tx *sql.Tx, oldName, newName string) (int, error) {
	for _, table := range []string{"chunk_vectors", "embeddings"} {
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM `+table+` WHERE chunk_id IN (
				SELECT c.id FROM chunks c
				JOIN documents old ON old.id = c.doc_id
				JOIN documents new ON new.collection = ? AND new.path = old.path
				                  AND new.content_hash = old.content_hash
				WHERE old.collection = ?
			)`, newName, oldName); err != nil {
			return 0, fmt.Errorf("deleting duplicate %s: %w", table, err)
		}
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM chunks WHERE doc_id IN (
			SELECT old.id FROM documents old
			JOIN documents new ON new.collection = ? AND new.path = old.path
			                  AND new.content_hash = old.content_hash
			WHERE old.collection = ?
		)`, newName, oldName); err != nil {
		return 0, fmt.Errorf("deleting duplicate chunks: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM documents AS old
		WHERE old.collection = ?
		  AND EXISTS (
			SELECT 1 FROM documents new
			WHERE new.collection = ? AND new.path = old.path
			  AND new.content_hash = old.content_hash
		  )
	`, oldName, newName); err != nil {
		return 0, fmt.Errorf("deleting duplicate documents: %w", err)
	}

	// Anything still sharing a path with the destination is a different file.
	// Leaving it under oldName loses nothing and keeps UNIQUE(collection, path).
	if _, err := tx.ExecContext(ctx, `
		UPDATE documents AS old SET collection = ?
		WHERE old.collection = ?
		  AND NOT EXISTS (
			SELECT 1 FROM documents new
			WHERE new.collection = ? AND new.path = old.path
		  )`, newName, oldName, newName); err != nil {
		return 0, fmt.Errorf("renaming documents: %w", err)
	}

	var stranded int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM documents WHERE collection = ?`, oldName).Scan(&stranded); err != nil {
		return 0, fmt.Errorf("counting unmoved documents: %w", err)
	}
	// Run history follows the documents, so it only moves when they all did.
	if stranded == 0 {
		if _, err := tx.ExecContext(ctx,
			`UPDATE index_runs SET collection = ? WHERE collection = ?`, newName, oldName); err != nil {
			return 0, fmt.Errorf("renaming index_runs: %w", err)
		}
	}

	return stranded, nil
}
