package indexer

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
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
	"vendor":       true,
	".tox":         true, ".mypy_cache": true, ".pytest_cache": true, "__pycache__": true,
	".ruff_cache": true, ".hypothesis": true,
	"target": true, // Rust/Java/Maven
	"dist":   true, "build": true, "out": true,
	".gradle": true, ".idea": true, ".vscode": true,
	".DS_Store": true,
}

// defaultExtensions are indexed when a collection doesn't specify extensions.
var defaultExtensions = map[string]bool{
	".md": true, ".markdown": true,
	".txt": true, ".text": true,
}

// Stats summarises an index run.
type Stats struct {
	FilesScanned int
	FilesAdded   int
	FilesUpdated int
	FilesRemoved int
	FilesSkipped int
	Duration     time.Duration
}

// Indexer walks a collection and upserts documents into the DB.
type Indexer struct {
	db         *db.DB
	chunker    chunker.Chunker
	beforeRead func(string) // test hook; called after discovery, before secure open
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

	// Canonicalize the collection root once so a configured symlinked root is
	// pinned to its target. Entries below it are never followed through links.
	// Keep col.Path/col.Name unchanged because document keys use the configured
	// collection identity.
	canonicalRoot, err := config.CanonicalPath(col.Path)
	if err != nil {
		canonicalRoot = filepath.Clean(col.Path)
	}

	root, err := openSecureRoot(canonicalRoot)
	if err != nil {
		runErr := fmt.Errorf("opening collection root %s: %w", col.Path, err)
		if finishErr := idx.finishRun(ctx, runID, stats, runErr); finishErr != nil {
			runErr = errors.Join(runErr, fmt.Errorf("finishing index run: %w", finishErr))
		}
		return stats, runErr
	}
	defer root.Close()

	// Track which paths we've seen to detect deletions and collect every file
	// failure rather than reporting a successful (but stale) collection run.
	seenPaths := map[string]bool{}
	var fileErrs []error

	err = filepath.WalkDir(canonicalRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			// Never skip the root itself based on its own basename — a
			// collection rooted at a dot-directory or a name that matches
			// the ignore/default-ignore sets (e.g. ~/.notes, ~/dist) would
			// otherwise return SkipDir immediately, yielding an empty
			// seenPaths and deactivating every document in the collection.
			if path != canonicalRoot &&
				(defaultIgnoreDirs[name] || ignoreSet[name] || (strings.HasPrefix(name, ".") && name != ".")) {
				return filepath.SkipDir
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if !allowedExts[ext] {
			return nil
		}

		// Never follow file symlinks. Containment checks followed by os.ReadFile
		// are racy: an attacker can replace the checked entry before it opens.
		// Regular files are opened descriptor-relatively below with O_NOFOLLOW
		// on every component.
		if d.Type()&fs.ModeSymlink != 0 {
			slog.Warn("skipping symlink", "path", path)
			stats.FilesSkipped++
			return nil
		}

		relPath, err := filepath.Rel(canonicalRoot, path)
		if err != nil {
			return err
		}

		stats.FilesScanned++
		seenPaths[relPath] = true

		if idx.beforeRead != nil {
			idx.beforeRead(path)
		}
		data, err := root.ReadFile(relPath)
		if err == nil {
			err = idx.indexFile(ctx, col, relPath, data, &stats)
		}
		if err != nil {
			err = fmt.Errorf("indexing %s: %w", relPath, err)
			slog.Warn("failed to index file", "path", relPath, "error", err)
			fileErrs = append(fileErrs, err)
		}

		return nil
	})
	if err != nil {
		runErr := errors.Join(errors.Join(fileErrs...), fmt.Errorf("walking %s: %w", col.Path, err))
		if finishErr := idx.finishRun(ctx, runID, stats, runErr); finishErr != nil {
			runErr = errors.Join(runErr, fmt.Errorf("finishing index run: %w", finishErr))
		}
		return stats, runErr
	}

	// Deactivate documents that no longer exist on disk
	removed, deactivateErr := idx.deactivateMissing(ctx, col.Name, seenPaths)
	stats.FilesRemoved = removed
	if deactivateErr != nil {
		fileErrs = append(fileErrs, fmt.Errorf("deactivating missing files: %w", deactivateErr))
	}

	stats.Duration = time.Since(start)
	runErr := errors.Join(fileErrs...)
	if finishErr := idx.finishRun(ctx, runID, stats, runErr); finishErr != nil {
		runErr = errors.Join(runErr, fmt.Errorf("finishing index run: %w", finishErr))
	}
	return stats, runErr
}

func (idx *Indexer) indexFile(ctx context.Context, col config.Collection, relPath string, data []byte, stats *Stats) error {
	hash := sha256sum(data)

	// Check if document exists (active or deactivated) and whether content changed
	var existingHash string
	var docID int64
	var existingActive int
	row := idx.db.QueryRowContext(ctx,
		`SELECT id, content_hash, active FROM documents WHERE collection=? AND path=?`,
		col.Name, relPath)
	if err := row.Scan(&docID, &existingHash, &existingActive); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("looking up existing document: %w", err)
	}

	if existingActive == 1 && existingHash == hash {
		return nil // unchanged
	}

	// Fast-path: previously deactivated document restored with byte-identical content.
	// Reactivate the row without touching chunks or embeddings — deleting chunks would
	// cascade into chunk_vectors/embeddings (migration 003) and force pointless re-embedding.
	if docID != 0 && existingActive == 0 && existingHash == hash {
		if _, err := idx.db.ExecContext(ctx,
			`UPDATE documents SET active=1, updated_at=datetime('now') WHERE id=?`, docID); err != nil {
			return fmt.Errorf("reactivating document: %w", err)
		}
		stats.FilesAdded++
		return nil
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
		// Update (or reactivate a previously deactivated document)
		_, err = tx.ExecContext(ctx,
			`UPDATE documents SET title=?, content_hash=?, active=1, updated_at=datetime('now') WHERE id=?`,
			title, hash, docID)
		if err != nil {
			return fmt.Errorf("updating document: %w", err)
		}
		newDocID = docID
		// Delete old chunks (FTS triggers handle cleanup)
		if _, err := tx.ExecContext(ctx, `DELETE FROM chunks WHERE doc_id=?`, docID); err != nil {
			return fmt.Errorf("deleting old chunks: %w", err)
		}
		if existingActive == 0 {
			stats.FilesAdded++
		} else {
			stats.FilesUpdated++
		}
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

	toDeactivate, err := missingDocumentIDs(rows, seen)
	if err != nil {
		return 0, err
	}

	var errs []error
	removed := 0
	for _, id := range toDeactivate {
		if _, err := idx.db.ExecContext(ctx,
			`UPDATE documents SET active=0, updated_at=datetime('now') WHERE id=?`, id); err != nil {
			errs = append(errs, fmt.Errorf("document %d: %w", id, err))
			continue
		}
		removed++
	}

	return removed, errors.Join(errs...)
}

type rowIterator interface {
	Next() bool
	Scan(...any) error
	Err() error
}

func missingDocumentIDs(rows rowIterator, seen map[string]bool) ([]int64, error) {
	var missing []int64
	for rows.Next() {
		var id int64
		var path string
		if err := rows.Scan(&id, &path); err != nil {
			return nil, fmt.Errorf("scanning active document: %w", err)
		}
		if !seen[path] {
			missing = append(missing, id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading active documents: %w", err)
	}
	return missing, nil
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
