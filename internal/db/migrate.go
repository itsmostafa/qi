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
	// is removed before commit and excluded from the applied-set query. Crucially,
	// the applied set is read only after this write lock is held, so waiters observe
	// migrations committed by the process ahead of them and cannot rerun 003.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning migration transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_version(version) VALUES (0)`); err != nil {
		return fmt.Errorf("acquiring migration lock: %w", err)
	}

	// Track applied versions as a set rather than a high-water mark: a database
	// missing one marker must still be able to reconcile it (see the 004
	// recovery below) without the runner's decision depending on which
	// migrations happen to sort after it.
	applied, err := appliedVersions(ctx, tx)
	if err != nil {
		return err
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
		if applied[ver] {
			continue
		}

		data, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", name, err)
		}

		// SQLite has no ALTER TABLE ADD COLUMN IF NOT EXISTS, so a migration
		// whose DDL committed without its schema_version marker (early builds
		// of 004 did this) would fail forever on re-run. Treat the column
		// already being present as "applied" and just record the marker.
		alreadyApplied := false
		if c, ok := addedColumns[ver]; ok {
			alreadyApplied, err = columnExists(ctx, tx, c.table, c.column)
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
		applied[ver] = true
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM schema_version WHERE version = 0`); err != nil {
		return fmt.Errorf("releasing migration lock: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing migrations: %w", err)
	}
	return nil
}

// appliedVersions reads the set of migration versions already recorded.
// Version 0 is the transaction-local lock row and is excluded.
func appliedVersions(ctx context.Context, tx *sql.Tx) (map[int]bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT version FROM schema_version WHERE version > 0`)
	if err != nil {
		return nil, fmt.Errorf("reading schema version: %w", err)
	}
	defer rows.Close()

	applied := map[int]bool{}
	for rows.Next() {
		var ver int
		if err := rows.Scan(&ver); err != nil {
			return nil, fmt.Errorf("reading schema version: %w", err)
		}
		applied[ver] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading schema version: %w", err)
	}
	return applied, nil
}

// addedColumns names one column added by each ALTER TABLE migration, used to
// detect a DDL-committed-but-unversioned state.
var addedColumns = map[int]struct{ table, column string }{
	4: {"embeddings", "fingerprint"},
	6: {"documents", "doc_timestamp"},
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
