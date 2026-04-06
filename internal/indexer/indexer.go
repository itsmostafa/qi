package indexer

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/itsmostafa/qi/internal/chunker"
	"github.com/itsmostafa/qi/internal/config"
	"github.com/itsmostafa/qi/internal/db"
	"github.com/itsmostafa/qi/internal/parser"
)

// defaultIgnoreDirs are skipped unconditionally (common VCS/tool/build directories).
var defaultIgnoreDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true,
	".venv": true, "venv": true, ".env": true,
	"node_modules": true,
	"vendor": true,
	".tox": true, ".mypy_cache": true, ".pytest_cache": true, "__pycache__": true,
	".ruff_cache": true, ".hypothesis": true,
	"target": true,        // Rust/Java/Maven
	"dist": true, "build": true, "out": true,
	".gradle": true, ".idea": true, ".vscode": true,
	".DS_Store": true,
}

// defaultExtensions are indexed when a collection doesn't specify extensions.
var defaultExtensions = map[string]bool{
	".md": true, ".markdown": true,
	".txt": true, ".text": true,
	".go": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true,
	".py": true, ".rs": true,
}

// Stats summarises an index run.
type Stats struct {
	FilesScanned int
	FilesAdded   int
	FilesUpdated int
	FilesRemoved int
	Duration     time.Duration
}

// Indexer walks a collection and upserts documents into the DB.
type Indexer struct {
	db      *db.DB
	chunker chunker.Chunker
}

func New(database *db.DB, chunkSize int) *Indexer {
	return &Indexer{
		db:      database,
		chunker: chunker.NewBreakpointChunker(chunkSize),
	}
}

// Index indexes all files in a collection.
func (idx *Indexer) Index(ctx context.Context, col config.Collection) (Stats, error) {
	start := time.Now()
	stats := Stats{}

	runID, err := idx.startRun(ctx, col.Name)
	if err != nil {
		return stats, err
	}

	// Determine allowed extensions
	allowedExts := defaultExtensions
	if len(col.Extensions) > 0 {
		allowedExts = make(map[string]bool)
		for _, ext := range col.Extensions {
			allowedExts[ext] = true
		}
	}

	ignoreSet := make(map[string]bool)
	for _, ig := range col.Ignore {
		ignoreSet[ig] = true
	}

	// Track which paths we've seen to detect deletions
	seenPaths := map[string]bool{}

	err = filepath.WalkDir(col.Path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if defaultIgnoreDirs[d.Name()] || ignoreSet[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if !allowedExts[ext] {
			return nil
		}

		relPath, err := filepath.Rel(col.Path, path)
		if err != nil {
			return err
		}

		stats.FilesScanned++
		seenPaths[relPath] = true

		if err := idx.indexFile(ctx, col, relPath, path, &stats); err != nil {
			slog.Warn("failed to index file", "path", relPath, "error", err)
		}

		return nil
	})
	if err != nil {
		_ = idx.finishRun(ctx, runID, stats, err)
		return stats, fmt.Errorf("walking %s: %w", col.Path, err)
	}

	// Deactivate documents that no longer exist on disk
	removed, err := idx.deactivateMissing(ctx, col.Name, seenPaths)
	if err != nil {
		slog.Warn("deactivating missing files", "error", err)
	}
	stats.FilesRemoved = removed

	stats.Duration = time.Since(start)
	_ = idx.finishRun(ctx, runID, stats, nil)
	return stats, nil
}

func (idx *Indexer) indexFile(ctx context.Context, col config.Collection, relPath, absPath string, stats *Stats) error {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return err
	}

	hash := sha256sum(data)

	// Check if document exists and unchanged
	var existingHash string
	var docID int64
	row := idx.db.QueryRowContext(ctx,
		`SELECT id, content_hash FROM documents WHERE collection=? AND path=? AND active=1`,
		col.Name, relPath)
	_ = row.Scan(&docID, &existingHash)

	if existingHash == hash {
		return nil // unchanged
	}

	// Upsert content
	if _, err := idx.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO content(hash, body) VALUES (?, ?)`,
		hash, data); err != nil {
		return fmt.Errorf("inserting content: %w", err)
	}

	// Parse + chunk
	ext := strings.ToLower(filepath.Ext(relPath))
	p := parser.For(ext)
	doc, err := p.Parse(relPath, data)
	if err != nil {
		return fmt.Errorf("parsing: %w", err)
	}
	chunks := idx.chunker.Chunk(doc)

	// Upsert document
	title := doc.Title
	if title == "" {
		title = filepath.Base(relPath)
	}

	tx, err := idx.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var newDocID int64
	if docID == 0 {
		// Insert
		res, err := tx.ExecContext(ctx,
			`INSERT INTO documents(collection, path, title, content_hash, active, indexed_at, updated_at)
			 VALUES (?, ?, ?, ?, 1, datetime('now'), datetime('now'))`,
			col.Name, relPath, title, hash)
		if err != nil {
			return fmt.Errorf("inserting document: %w", err)
		}
		newDocID, _ = res.LastInsertId()
		stats.FilesAdded++
	} else {
		// Update
		_, err = tx.ExecContext(ctx,
			`UPDATE documents SET title=?, content_hash=?, updated_at=datetime('now') WHERE id=?`,
			title, hash, docID)
		if err != nil {
			return fmt.Errorf("updating document: %w", err)
		}
		newDocID = docID
		// Delete old chunks (FTS triggers handle cleanup)
		if _, err := tx.ExecContext(ctx, `DELETE FROM chunks WHERE doc_id=?`, docID); err != nil {
			return fmt.Errorf("deleting old chunks: %w", err)
		}
		stats.FilesUpdated++
	}

	// Insert chunks
	for _, ch := range chunks {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO chunks(content_hash, doc_id, seq, text, heading_path, ordinal, content_length)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			hash, newDocID, ch.Seq, ch.Text, ch.HeadingPath, ch.Ordinal, len(ch.Text))
		if err != nil {
			return fmt.Errorf("inserting chunk: %w", err)
		}
	}

	return tx.Commit()
}

func (idx *Indexer) deactivateMissing(ctx context.Context, collection string, seen map[string]bool) (int, error) {
	rows, err := idx.db.QueryContext(ctx,
		`SELECT id, path FROM documents WHERE collection=? AND active=1`, collection)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var toDeactivate []int64
	for rows.Next() {
		var id int64
		var path string
		if err := rows.Scan(&id, &path); err != nil {
			continue
		}
		if !seen[path] {
			toDeactivate = append(toDeactivate, id)
		}
	}

	for _, id := range toDeactivate {
		_, err := idx.db.ExecContext(ctx,
			`UPDATE documents SET active=0, updated_at=datetime('now') WHERE id=?`, id)
		if err != nil {
			slog.Warn("deactivating document", "id", id, "error", err)
		}
	}

	return len(toDeactivate), nil
}

func (idx *Indexer) startRun(ctx context.Context, collection string) (int64, error) {
	res, err := idx.db.ExecContext(ctx,
		`INSERT INTO index_runs(collection) VALUES (?)`, collection)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (idx *Indexer) finishRun(ctx context.Context, runID int64, stats Stats, runErr error) error {
	var errStr sql.NullString
	if runErr != nil {
		errStr = sql.NullString{String: runErr.Error(), Valid: true}
	}
	_, err := idx.db.ExecContext(ctx,
		`UPDATE index_runs SET finished_at=datetime('now'), files_scanned=?, files_added=?, files_updated=?, files_removed=?, error=? WHERE id=?`,
		stats.FilesScanned, stats.FilesAdded, stats.FilesUpdated, stats.FilesRemoved, errStr, runID)
	return err
}

func sha256sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
