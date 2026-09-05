package indexer

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
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

// maxFileSize caps a single indexed file. Reading, parsing and chunking hold
// several full-size copies at once — one 50 MiB file peaked at ~543 MiB RSS.
// ponytail: fixed constant, no knob. A `max_file_size` key on
// config.Collection is the upgrade path if anyone needs a different cap.
const maxFileSize = 10 << 20 // 10 MiB

// dateLayout is the storage format of documents.doc_timestamp. The recency
// filters compare it as a string, so it must stay lexicographically ordered.
const dateLayout = "2006-01-02"

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
	db      *db.DB
	chunker chunker.Chunker
	// Force reparses files whose content is unchanged. Change detection is by
	// content hash, so a parser or chunker fix is otherwise invisible to
	// everything already indexed.
	Force      bool
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
	var failedPaths []string

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
			var info fs.FileInfo
			if info, err = d.Info(); err == nil {
				err = idx.indexFile(ctx, col, relPath, data, info.ModTime(), &stats)
			}
		}
		if err != nil {
			err = fmt.Errorf("indexing %s: %w", relPath, err)
			slog.Warn("failed to index file", "path", relPath, "error", err)
			fileErrs = append(fileErrs, err)
			failedPaths = append(failedPaths, relPath)
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

	// A file that failed to read keeps its previous version active, deliberately
	// — a transient failure must not delete good data. Say so, or the run reports
	// an error with no hint that stale text is still being served.
	if stale := idx.staleActivePaths(ctx, col.Name, failedPaths); len(stale) > 0 {
		fileErrs = append(fileErrs, fmt.Errorf(
			"%d document(s) could not be re-read; their previously indexed content is still searchable: %s",
			len(stale), strings.Join(stale, ", ")))
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

	// Housekeeping is best-effort: a full index run should not fail because the
	// database could not be compacted afterwards.
	if err := idx.compact(ctx); err != nil {
		slog.Warn("compacting database after index run", "error", err)
	}
	return stats, runErr
}

// compact reclaims the space an index run leaves behind. Without it the file
// only ever grows: FTS5 keeps every segment it has ever written, superseded
// document bodies are never referenced again, and SQLite hands freed pages to a
// freelist it never returns to the filesystem.
func (idx *Indexer) compact(ctx context.Context) error {
	// A document deactivated because its file was deleted from disk keeps its
	// plaintext body — including removed secrets — for the reactivation
	// fast-path in indexFile. Nothing else reads active = 0 (search, stats, get
	// and the embedder all filter active = 1), so drop the rows and let the
	// orphan prune below reclaim the text.
	// ponytail: costs the reactivation fast-path — a file that comes back is
	// re-chunked and re-embedded instead of reactivated. A retention window
	// (keep bodies for N days after deactivation) is the upgrade path.
	// One transaction: compact errors are logged, not returned, so a chunk
	// delete that committed without its document delete would leave an inactive
	// row with no chunks for the reactivation fast-path to restore.
	if err := func() error {
		tx, err := idx.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM chunks WHERE doc_id IN (SELECT id FROM documents WHERE active = 0)`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM documents WHERE active = 0`); err != nil {
			return err
		}
		return tx.Commit()
	}(); err != nil {
		return fmt.Errorf("deleting deactivated documents: %w", err)
	}

	// A document that changed content leaves its previous body behind, holding
	// superseded text — including anything since redacted — in the database.
	if _, err := idx.db.ExecContext(ctx, `
		DELETE FROM content WHERE hash NOT IN (
			SELECT DISTINCT content_hash FROM documents
		)`); err != nil {
		return fmt.Errorf("pruning orphaned content: %w", err)
	}

	// FTS5 merges segments only when asked. Reindex cycles otherwise leave
	// thousands of unmerged segments — 27 MiB of index for 241 chunks, in the
	// case that prompted this.
	if _, err := idx.db.ExecContext(ctx,
		`INSERT INTO chunks_fts(chunks_fts) VALUES('optimize')`); err != nil {
		return fmt.Errorf("optimizing fts index: %w", err)
	}

	var pageCount, freelist int64
	if err := idx.db.QueryRowContext(ctx,
		`SELECT * FROM pragma_page_count(), pragma_freelist_count()`).Scan(&pageCount, &freelist); err != nil {
		return fmt.Errorf("reading page counts: %w", err)
	}
	// VACUUM rewrites the whole file, so only pay for it once the dead space is
	// worth reclaiming.
	if freelist < 1000 || freelist < pageCount/4 {
		return nil
	}
	slog.Info("vacuuming database", "free_pages", freelist, "total_pages", pageCount)
	if _, err := idx.db.ExecContext(ctx, `VACUUM`); err != nil {
		return fmt.Errorf("vacuuming: %w", err)
	}
	return nil
}

func (idx *Indexer) indexFile(ctx context.Context, col config.Collection, relPath string, data []byte, modTime time.Time, stats *Stats) error {
	hash := sha256sum(data)

	// Check if document exists (active or deactivated) and whether content changed
	var existingHash string
	var docID int64
	var existingActive int
	var existingTime sql.NullString
	row := idx.db.QueryRowContext(ctx,
		`SELECT id, content_hash, active, doc_timestamp FROM documents WHERE collection=? AND path=?`,
		col.Name, relPath)
	if err := row.Scan(&docID, &existingHash, &existingActive, &existingTime); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("looking up existing document: %w", err)
	}

	if existingActive == 1 && existingHash == hash && !idx.Force {
		if existingTime.Valid {
			return nil // unchanged
		}
		// Rows indexed before dates existed have a NULL timestamp and are
		// invisible to --since/--until forever, since unchanged content never
		// reaches the write path below. Backfill without re-chunking or
		// re-embedding: parsing costs one pass, and only until it succeeds.
		doc, err := parser.For(strings.ToLower(filepath.Ext(relPath))).Parse(relPath, data)
		if err != nil {
			return fmt.Errorf("parsing: %w", err)
		}
		if _, err := idx.db.ExecContext(ctx,
			`UPDATE documents SET doc_timestamp=? WHERE id=?`,
			documentDate(doc.Meta.Timestamp, modTime), docID); err != nil {
			return fmt.Errorf("backfilling document date: %w", err)
		}
		return nil
	}

	// Fast-path: previously deactivated document restored with byte-identical content.
	// Reactivate the row without touching chunks or embeddings — deleting chunks would
	// cascade into chunk_vectors/embeddings (migration 003) and force pointless re-embedding.
	// A row with no date is not fully indexed: reactivating it would restore a
	// document that no date filter can ever match. Let it fall through and be
	// rebuilt instead.
	if docID != 0 && existingActive == 0 && existingHash == hash && existingTime.Valid && !idx.Force {
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
	docTime := documentDate(doc.Meta.Timestamp, modTime)
	var tagsJSON any
	if len(doc.Meta.Tags) > 0 {
		if b, err := json.Marshal([]string(doc.Meta.Tags)); err == nil {
			tagsJSON = string(b)
		}
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
			`INSERT INTO documents(collection, path, title, content_hash, doc_timestamp, tags, active, indexed_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, 1, datetime('now'), datetime('now'))`,
			col.Name, relPath, title, hash, docTime, tagsJSON)
		if err != nil {
			return fmt.Errorf("inserting document: %w", err)
		}
		newDocID, _ = res.LastInsertId()
		stats.FilesAdded++
	} else {
		// Update (or reactivate a previously deactivated document)
		_, err = tx.ExecContext(ctx,
			`UPDATE documents SET title=?, content_hash=?, doc_timestamp=?, tags=?, active=1, updated_at=datetime('now') WHERE id=?`,
			title, hash, docTime, tagsJSON, docID)
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

// staleActivePaths returns the failed paths whose previously indexed document
// is still active, i.e. still searchable with content this run could not verify.
// ponytail: one query per failure; failures are rare, an IN-list if that changes.
func (idx *Indexer) staleActivePaths(ctx context.Context, collection string, failed []string) []string {
	var stale []string
	for _, p := range failed {
		var n int
		if err := idx.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM documents WHERE collection=? AND path=? AND active=1`,
			collection, p).Scan(&n); err == nil && n > 0 {
			stale = append(stale, p)
		}
	}
	return stale
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

// documentDate is the document's date for recency filters: the frontmatter
// date when there is a readable one, otherwise the file's modification time.
// Without the fallback every undated document is NULL, and a NULL never
// satisfies --since or --until, so the filters silently returned nothing on
// corpora that do not use dated frontmatter. Local time, so an 11pm save is
// not filed under tomorrow.
func documentDate(frontmatter string, modTime time.Time) string {
	if d := normalizeDate(frontmatter); d != "" {
		return d
	}
	return modTime.Local().Format(dateLayout)
}

// normalizeDate reduces a frontmatter date to YYYY-MM-DD, or "" when there is
// nothing parseable. Anything else is dropped rather than stored in a shape
// date filters cannot compare.
func normalizeDate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, layout := range []string{dateLayout, time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format(dateLayout)
		}
	}
	if len(s) >= 10 {
		if t, err := time.Parse(dateLayout, s[:10]); err == nil {
			return t.Format(dateLayout)
		}
	}
	return ""
}

func sha256sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
