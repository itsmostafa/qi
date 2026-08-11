package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func runMigrations(ctx context.Context, db *sql.DB) error {
	// Ensure schema_version table exists before querying it
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_version (
		version    INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`)
	if err != nil {
		return fmt.Errorf("creating schema_version: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("reading migrations dir: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	// A write statement before reading schema_version serializes migration
	// runners across processes. Version 0 is a transaction-local lock row: it
	// is removed before commit and excluded from the version query. Crucially,
	// the version is read only after this write lock is held, so waiters observe
	// migrations committed by the process ahead of them and cannot rerun 003.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning migration transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_version(version) VALUES (0)`); err != nil {
		return fmt.Errorf("acquiring migration lock: %w", err)
	}

	var current int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_version WHERE version > 0`).Scan(&current); err != nil {
		return fmt.Errorf("reading schema version: %w", err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		ver, err := parseMigrationVersion(name)
		if err != nil {
			return fmt.Errorf("parsing migration name %q: %w", name, err)
		}
		if ver <= current {
			continue
		}

		data, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", name, err)
		}

		// Early builds of migration 004 could commit the ALTER TABLE before
		// recording schema_version. Treat that exact state as already applied;
		// all new applications execute the DDL and version marker atomically.
		alreadyApplied := false
		if ver == 4 {
			alreadyApplied, err = columnExists(ctx, tx, "embeddings", "fingerprint")
		}
		if err == nil && !alreadyApplied {
			_, err = tx.ExecContext(ctx, string(data))
		}
		if err == nil && alreadyApplied {
			_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_version(version) VALUES (?)`, ver)
		}
		if err != nil {
			return fmt.Errorf("applying migration %s: %w", name, err)
		}
		current = ver
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM schema_version WHERE version = 0`); err != nil {
		return fmt.Errorf("releasing migration lock: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing migrations: %w", err)
	}
	return nil
}

func columnExists(ctx context.Context, tx *sql.Tx, table, column string) (bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func parseMigrationVersion(name string) (int, error) {
	// Expect names like "001_init.sql"
	parts := strings.SplitN(name, "_", 2)
	if len(parts) < 1 {
		return 0, fmt.Errorf("unexpected format")
	}
	return strconv.Atoi(parts[0])
}
