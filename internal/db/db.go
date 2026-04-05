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
