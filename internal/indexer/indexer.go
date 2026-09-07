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

	// Detect legacy ranges once per collection, not once per unchanged file.
	rangeRepairs, err := idx.chunkRangeRepairs(ctx, col.Name)
	if err != nil {
		return stats, fmt.Errorf("checking chunk line ranges: %w", err)
	}
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
				err = idx.indexFile(ctx, col, relPath, data, info.ModTime(), rangeRepairs[relPath], &stats)
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

func (idx *Indexer) indexFile(ctx context.Context, col config.Collection, relPath string, data []byte, modTime time.Time, rangeRepairRequired bool, stats *Stats) error {
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

	var doc *parser.Document
	var chunks []chunker.Chunk
	var err error
	if existingActive == 1 && existingHash == hash && !idx.Force {
		// Unchanged bytes still need their date rechecked. Two rows land here:
		// one indexed before dates existed, holding a NULL that no --since or
		// --until can ever match; and one whose date came from the mtime, which
		// has since moved without the content changing (touch, cloud sync, git
		// checkout), leaving the stored date disagreeing with the filesystem
		// that defines the fallback. Both are fixed by re-deriving the date —
		// no re-chunking, no re-embedding.
		//
		// The parse is the only way to tell a frontmatter date from a fallback
		// one, so skip it when the current mtime already explains what is
		// stored.
		// ponytail: parses every frontmatter-dated file each run; export a
		// frontmatter-only date reader from parser if index time starts to hurt.
		if !rangeRepairRequired && existingTime.Valid && existingTime.String == documentDate("", modTime) {
			return nil // unchanged
		}
		doc, err = parser.For(strings.ToLower(filepath.Ext(relPath))).Parse(relPath, data)
		if err != nil {
			return fmt.Errorf("parsing: %w", err)
		}
		if rangeRepairRequired {
			// Keep embeddings only when the chunk layout is identical. Reuse
			// this parse and chunking below if a rebuild is needed instead.
			chunks = idx.chunker.Chunk(doc)
			backfilled, err := idx.backfillChunkRanges(ctx, docID, chunks)
			if err != nil {
				return fmt.Errorf("backfilling chunk line ranges: %w", err)
			}
			rangeRepairRequired = !backfilled
		}
		docDate := documentDate(doc.Meta.Timestamp, modTime)
		if !rangeRepairRequired {
			if docDate == existingTime.String {
				return nil // frontmatter still supplies the date; mtime is irrelevant
			}
			if _, err := idx.db.ExecContext(ctx,
				`UPDATE documents SET doc_timestamp=? WHERE id=?`,
				docDate, docID); err != nil {
				return fmt.Errorf("updating document date: %w", err)
			}
			return nil
		}
	}

	// Fast-path: previously deactivated document restored with byte-identical content.
	// Reactivate the row without touching chunks or embeddings — deleting chunks would
	// cascade into chunk_vectors/embeddings (migration 003) and force pointless re-embedding.
	// A row with no date is not fully indexed: reactivating it would restore a
	// document that no date filter can ever match. Let it fall through and be
	// rebuilt instead.
	if docID != 0 && existingActive == 0 && existingHash == hash && existingTime.Valid && !idx.Force && !rangeRepairRequired {
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

	// Parse + chunk unless the range repair already did it.
	if doc == nil {
		doc, err = parser.For(strings.ToLower(filepath.Ext(relPath))).Parse(relPath, data)
		if err != nil {
			return fmt.Errorf("parsing: %w", err)
		}
		chunks = idx.chunker.Chunk(doc)
	}
	for _, ch := range chunks {
		if ch.StartLine <= 0 || ch.EndLine < ch.StartLine {
			return fmt.Errorf("invalid source line range for chunk %d: %d-%d", ch.Seq, ch.StartLine, ch.EndLine)
		}
	}

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
			`INSERT INTO chunks(content_hash, doc_id, seq, text, heading_path, ordinal, content_length, start_line, end_line)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			hash, newDocID, ch.Seq, ch.Text, ch.HeadingPath, ch.Ordinal, len(ch.Text), ch.StartLine, ch.EndLine)
		if err != nil {
			return fmt.Errorf("inserting chunk: %w", err)
		}
	}

	return tx.Commit()
}

// chunkRangeRepairs finds documents with unknown or invalid source ranges.
// Empty documents have no chunks and need no repair. Include inactive rows so
// the reactivation fast path cannot restore chunks without usable citations.
func (idx *Indexer) chunkRangeRepairs(ctx context.Context, collection string) (map[string]bool, error) {
	rows, err := idx.db.QueryContext(ctx, `
		SELECT DISTINCT d.path FROM chunks c JOIN documents d ON d.id=c.doc_id
		WHERE d.collection=? AND
		(c.start_line IS NULL OR c.end_line IS NULL OR c.start_line < 1 OR c.end_line < c.start_line)`, collection)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	repairs := map[string]bool{}
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		repairs[path] = true
	}
	return repairs, rows.Err()
}

// backfillChunkRanges updates legacy chunks in place when the current parser
// and chunker produce exactly the same chunk text and metadata. Ordinal is not
// part of the identity: older indexers recorded a section start, while the
// current chunker records the chunk start. Updating it here is safe because
// embeddings are generated from chunk text (and heading/text identity is
// unchanged). If anything else differs, callers rebuild the chunks and the
// embedding repair pass regenerates vectors.
func (idx *Indexer) backfillChunkRanges(ctx context.Context, docID int64, chunks []chunker.Chunk) (bool, error) {
	type storedChunk struct {
		id, seq, length int64
		text, heading   string
		headingValid    bool
	}

	rows, err := idx.db.QueryContext(ctx, `
		SELECT id, seq, text, heading_path, content_length
		FROM chunks WHERE doc_id=? ORDER BY seq`, docID)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	stored := make([]storedChunk, 0, len(chunks))
	for rows.Next() {
		var c storedChunk
		var headingText sql.NullString
		if err := rows.Scan(&c.id, &c.seq, &c.text, &headingText, &c.length); err != nil {
			return false, err
		}
		c.headingValid, c.heading = headingText.Valid, headingText.String
		stored = append(stored, c)
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	if err := rows.Close(); err != nil {
		return false, err
	}
	if len(stored) == 0 || len(stored) != len(chunks) {
		return false, nil
	}

	for i, old := range stored {
		ch := chunks[i]
		if ch.StartLine <= 0 || ch.EndLine < ch.StartLine {
			return false, fmt.Errorf("invalid source line range for chunk %d: %d-%d", ch.Seq, ch.StartLine, ch.EndLine)
		}
		if old.seq != int64(ch.Seq) || old.text != ch.Text ||
			!old.headingValid || old.heading != ch.HeadingPath ||
			old.length != int64(len(ch.Text)) {
			return false, nil
		}
	}

	// Update source locations from the current chunker. This transaction leaves
	// chunk IDs and their vector/embedding rows intact when the exact-match
	// proof succeeds.
	tx, err := idx.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	for i, old := range stored {
		ch := chunks[i]
		if _, err := tx.ExecContext(ctx,
			`UPDATE chunks SET ordinal=?, start_line=?, end_line=? WHERE id=?`,
			ch.Ordinal, ch.StartLine, ch.EndLine, old.id); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
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
