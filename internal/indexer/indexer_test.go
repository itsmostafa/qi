package indexer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/itsmostafa/qi/internal/config"
	"github.com/itsmostafa/qi/internal/db"
	"github.com/itsmostafa/qi/internal/parser"
)

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "idx_test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func makeTestCollection(t *testing.T, files map[string]string) config.Collection {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	return config.Collection{
		Name:       "test",
		Path:       dir,
		Extensions: []string{".md", ".txt"},
	}
}

func TestIndexer_AddFiles(t *testing.T) {
	database := openTestDB(t)
	idx := New(database, 256)
	col := makeTestCollection(t, map[string]string{
		"a.md":  "# Doc A\nContent of document A.",
		"b.txt": "Document B plain text.",
	})

	stats, err := idx.Index(context.Background(), col)
	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}
	if stats.FilesScanned != 2 {
		t.Errorf("expected 2 scanned, got %d", stats.FilesScanned)
	}
	if stats.FilesAdded != 2 {
		t.Errorf("expected 2 added, got %d", stats.FilesAdded)
	}
	if stats.FilesUpdated != 0 {
		t.Errorf("expected 0 updated, got %d", stats.FilesUpdated)
	}
}

func TestIndexer_IncrementalUpdate(t *testing.T) {
	database := openTestDB(t)
	idx := New(database, 256)
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")

	if err := os.WriteFile(path, []byte("# Original\nOriginal content."), 0o640); err != nil {
		t.Fatal(err)
	}

	col := config.Collection{Name: "test", Path: dir, Extensions: []string{".md"}}

	// First index
	stats, err := idx.Index(context.Background(), col)
	if err != nil {
		t.Fatal(err)
	}
	if stats.FilesAdded != 1 {
		t.Errorf("expected 1 added, got %d", stats.FilesAdded)
	}

	// Second index — no changes
	stats, err = idx.Index(context.Background(), col)
	if err != nil {
		t.Fatal(err)
	}
	if stats.FilesUpdated != 0 || stats.FilesAdded != 0 {
		t.Errorf("expected 0 changes, got added=%d updated=%d", stats.FilesAdded, stats.FilesUpdated)
	}

	// Modify file
	if err := os.WriteFile(path, []byte("# Updated\nUpdated content."), 0o640); err != nil {
		t.Fatal(err)
	}

	stats, err = idx.Index(context.Background(), col)
	if err != nil {
		t.Fatal(err)
	}
	if stats.FilesUpdated != 1 {
		t.Errorf("expected 1 updated, got %d", stats.FilesUpdated)
	}
}

type countingParser struct {
	parser.Parser
	calls int
}

func (p *countingParser) Parse(path string, data []byte) (*parser.Document, error) {
	p.calls++
	return p.Parser.Parse(path, data)
}

func TestIndexer_PersistsAndRepairsSourceLineRanges(t *testing.T) {
	p := &countingParser{Parser: parser.For(".md")}
	parser.Register(".md", p)
	t.Cleanup(func() { parser.Register(".md", p.Parser) })
	database := openTestDB(t)
	idx := New(database, 256)
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	col := config.Collection{Name: "test", Path: dir, Extensions: []string{".md"}}
	body := []byte("# Heading\none\ntwo")
	if err := os.WriteFile(path, body, 0o640); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := idx.Index(ctx, col); err != nil {
		t.Fatal(err)
	}

	var chunkID int64
	var startLine, endLine int
	if err := database.QueryRowContext(ctx, `
		SELECT c.id, c.start_line, c.end_line FROM chunks c
		JOIN documents d ON d.id=c.doc_id WHERE d.path='doc.md'`).Scan(&chunkID, &startLine, &endLine); err != nil {
		t.Fatalf("reading line range: %v", err)
	}
	if startLine != 2 || endLine != 3 {
		t.Fatalf("got line range %d-%d, want 2-3", startLine, endLine)
	}

	// Simulate a pre-migration chunk. An exact layout match must repair only
	// the nullable range and retain the chunk ID and its embedding.
	if err := database.InsertEmbedding(ctx, chunkID, []float32{0.1, 0.2}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx,
		`INSERT INTO embeddings(chunk_id, provider, model, dimension) VALUES (?, 'test', 'test-model', 2)`, chunkID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx,
		`UPDATE chunks SET start_line=NULL, end_line=NULL WHERE id=?`, chunkID); err != nil {
		t.Fatal(err)
	}

	p.calls = 0
	stats, err := idx.Index(ctx, col)
	if err != nil {
		t.Fatalf("legacy range repair failed: %v", err)
	}
	if p.calls != 1 {
		t.Fatalf("range repair parsed %d times, want 1", p.calls)
	}
	if stats.FilesUpdated != 0 || stats.FilesAdded != 0 {
		t.Fatalf("range repair changed file stats: added=%d updated=%d", stats.FilesAdded, stats.FilesUpdated)
	}
	var repairedID int64
	if err := database.QueryRowContext(ctx,
		`SELECT c.id FROM chunks c JOIN documents d ON d.id=c.doc_id WHERE d.path='doc.md'`).Scan(&repairedID); err != nil {
		t.Fatal(err)
	}
	if repairedID != chunkID {
		t.Fatalf("range repair replaced chunk %d with %d", chunkID, repairedID)
	}
	if err := database.QueryRowContext(ctx,
		`SELECT start_line, end_line FROM chunks WHERE id=?`, chunkID).Scan(&startLine, &endLine); err != nil {
		t.Fatal(err)
	}
	if startLine != 2 || endLine != 3 {
		t.Fatalf("repaired line range %d-%d, want 2-3", startLine, endLine)
	}
	var vectorCount int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM chunk_vectors WHERE chunk_id=?`, chunkID).Scan(&vectorCount); err != nil {
		t.Fatal(err)
	}
	if vectorCount != 1 {
		t.Fatalf("range repair dropped embedding, count=%d", vectorCount)
	}

	// If the old chunk layout cannot be proven equivalent, a matching source
	// hash must still rebuild instead of taking the unchanged-date fast path.
	if _, err := database.ExecContext(ctx,
		`UPDATE chunks SET text='stale', start_line=NULL, end_line=NULL WHERE id=?`, chunkID); err != nil {
		t.Fatal(err)
	}
	p.calls = 0
	stats, err = idx.Index(ctx, col)
	if err != nil {
		t.Fatalf("legacy layout rebuild failed: %v", err)
	}
	if p.calls != 1 {
		t.Fatalf("layout rebuild parsed %d times, want 1", p.calls)
	}
	if stats.FilesUpdated != 1 {
		t.Fatalf("expected one updated file after unsafe legacy repair, got %d", stats.FilesUpdated)
	}
	if err := database.QueryRowContext(ctx,
		`SELECT c.id, c.start_line, c.end_line FROM chunks c
		 JOIN documents d ON d.id=c.doc_id WHERE d.path='doc.md'`).Scan(&repairedID, &startLine, &endLine); err != nil {
		t.Fatal(err)
	}
	if repairedID == chunkID || startLine != 2 || endLine != 3 {
		t.Fatalf("unsafe repair left chunk id/range as %d/%d-%d", repairedID, startLine, endLine)
	}
}

func TestIndexer_ReindexWithEmbeddings(t *testing.T) {
	database := openTestDB(t)
	idx := New(database, 256)
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	col := config.Collection{Name: "test", Path: dir, Extensions: []string{".md"}}

	if err := os.WriteFile(path, []byte("# Original\nOriginal content."), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := idx.Index(context.Background(), col); err != nil {
		t.Fatal(err)
	}

	// Find a chunk for this document and insert a fake embedding.
	var chunkID int64
	row := database.QueryRowContext(context.Background(),
		`SELECT c.id FROM chunks c JOIN documents d ON d.id=c.doc_id
		 WHERE d.collection='test' AND d.path='doc.md' LIMIT 1`)
	if err := row.Scan(&chunkID); err != nil {
		t.Fatalf("finding chunk: %v", err)
	}
	if err := database.InsertEmbedding(context.Background(), chunkID, []float32{0.1, 0.2, 0.3, 0.4}); err != nil {
		t.Fatalf("inserting chunk_vector: %v", err)
	}
	if _, err := database.ExecContext(context.Background(),
		`INSERT INTO embeddings(chunk_id, provider, model, dimension) VALUES (?, 'test', 'test-model', 4)`,
		chunkID); err != nil {
		t.Fatalf("inserting embeddings row: %v", err)
	}

	// Modify the file so its hash changes.
	if err := os.WriteFile(path, []byte("# Updated\nUpdated content."), 0o640); err != nil {
		t.Fatal(err)
	}

	// Reindex must succeed and report the file as updated.
	stats, err := idx.Index(context.Background(), col)
	if err != nil {
		t.Fatalf("reindex failed: %v", err)
	}
	if stats.FilesUpdated != 1 {
		t.Errorf("expected 1 updated, got %d", stats.FilesUpdated)
	}

	// No orphaned rows should remain in chunk_vectors or embeddings.
	var orphanVectors int
	if err := database.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM chunk_vectors WHERE chunk_id NOT IN (SELECT id FROM chunks)`).Scan(&orphanVectors); err != nil {
		t.Fatalf("querying orphan chunk_vectors: %v", err)
	}
	if orphanVectors != 0 {
		t.Errorf("expected 0 orphan chunk_vectors rows, got %d", orphanVectors)
	}

	var orphanEmbeddings int
	if err := database.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM embeddings WHERE chunk_id NOT IN (SELECT id FROM chunks)`).Scan(&orphanEmbeddings); err != nil {
		t.Fatalf("querying orphan embeddings: %v", err)
	}
	if orphanEmbeddings != 0 {
		t.Errorf("expected 0 orphan embeddings rows, got %d", orphanEmbeddings)
	}
}

func TestIndexer_ReindexAfterDeletion(t *testing.T) {
	database := openTestDB(t)
	idx := New(database, 256)
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	col := config.Collection{Name: "test", Path: dir, Extensions: []string{".md"}}

	// Index the file
	if err := os.WriteFile(path, []byte("# Original\nOriginal content."), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := idx.Index(context.Background(), col); err != nil {
		t.Fatal(err)
	}

	// Delete the file → deactivates the document
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	stats, err := idx.Index(context.Background(), col)
	if err != nil {
		t.Fatal(err)
	}
	if stats.FilesRemoved != 1 {
		t.Fatalf("expected 1 removed, got %d", stats.FilesRemoved)
	}

	// Restore the file with new content
	if err := os.WriteFile(path, []byte("# Restored\nRestored content."), 0o640); err != nil {
		t.Fatal(err)
	}
	stats, err = idx.Index(context.Background(), col)
	if err != nil {
		t.Fatalf("index after restore: %v", err)
	}
	if stats.FilesAdded != 1 {
		t.Errorf("expected 1 added after restore, got %d", stats.FilesAdded)
	}

	// Verify the document is active with new hash and has chunks
	var active int
	var hash string
	row := database.QueryRowContext(context.Background(),
		`SELECT active, content_hash FROM documents WHERE collection='test' AND path='doc.md'`)
	if err := row.Scan(&active, &hash); err != nil {
		t.Fatalf("querying restored document: %v", err)
	}
	if active != 1 {
		t.Errorf("expected active=1, got %d", active)
	}

	var chunkCount int
	cRow := database.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM chunks c JOIN documents d ON d.id=c.doc_id WHERE d.collection='test' AND d.path='doc.md' AND d.active=1`)
	if err := cRow.Scan(&chunkCount); err != nil {
		t.Fatalf("querying chunks: %v", err)
	}
	if chunkCount == 0 {
		t.Error("expected at least one chunk after restore")
	}
}

// compact hard-deletes deactivated documents, so the reactivation fast-path in
// indexFile no longer fires: a restored file is indexed from scratch. That is
// the accepted cost of not keeping a removed file's plaintext body around.
func TestIndexer_RestoredFileReindexesFromScratch(t *testing.T) {
	database := openTestDB(t)
	idx := New(database, 256)
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	col := config.Collection{Name: "test", Path: dir, Extensions: []string{".md"}}
	ctx := context.Background()

	body := []byte("# Original\nOriginal content.")
	if err := os.WriteFile(path, body, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := idx.Index(ctx, col); err != nil {
		t.Fatal(err)
	}

	// Remove the file, then restore byte-identical content.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := idx.Index(ctx, col); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o640); err != nil {
		t.Fatal(err)
	}
	stats, err := idx.Index(ctx, col)
	if err != nil {
		t.Fatalf("index after restore: %v", err)
	}
	if stats.FilesAdded != 1 {
		t.Errorf("expected 1 added, got %d", stats.FilesAdded)
	}

	var active, chunks int
	if err := database.QueryRowContext(ctx,
		`SELECT active FROM documents WHERE collection='test' AND path='doc.md'`).Scan(&active); err != nil {
		t.Fatalf("querying restored document: %v", err)
	}
	if active != 1 {
		t.Errorf("expected active=1, got %d", active)
	}
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM chunks c JOIN documents d ON d.id=c.doc_id
		 WHERE d.collection='test' AND d.path='doc.md'`).Scan(&chunks); err != nil {
		t.Fatalf("querying chunks: %v", err)
	}
	if chunks == 0 {
		t.Error("restored document has no chunks")
	}
}

func TestIndexer_DeactivatesMissingFiles(t *testing.T) {
	database := openTestDB(t)
	idx := New(database, 256)
	dir := t.TempDir()

	// Create two files
	for _, name := range []string{"keep.md", "delete.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("# "+name), 0o640); err != nil {
			t.Fatal(err)
		}
	}

	col := config.Collection{Name: "test", Path: dir, Extensions: []string{".md"}}
	if _, err := idx.Index(context.Background(), col); err != nil {
		t.Fatal(err)
	}

	// Remove one file
	if err := os.Remove(filepath.Join(dir, "delete.md")); err != nil {
		t.Fatal(err)
	}

	stats, err := idx.Index(context.Background(), col)
	if err != nil {
		t.Fatal(err)
	}
	if stats.FilesRemoved != 1 {
		t.Errorf("expected 1 removed, got %d", stats.FilesRemoved)
	}
}

// One 50 MiB file peaked at ~543 MiB RSS; maxFileSize keeps it out entirely.
func TestIndexer_RejectsOversizeFile(t *testing.T) {
	database := openTestDB(t)
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "big.md"))
	if err != nil {
		t.Fatal(err)
	}
	// Sparse: the cap is checked from the size, nothing is read.
	if err := f.Truncate(maxFileSize + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()

	col := config.Collection{Name: "test", Path: dir, Extensions: []string{".md"}}
	_, err = New(database, 256).Index(context.Background(), col)
	if err == nil || !strings.Contains(err.Error(), "over the") {
		t.Fatalf("expected an over-the-limit error, got %v", err)
	}

	var docs int
	if err := database.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM documents`).Scan(&docs); err != nil {
		t.Fatal(err)
	}
	if docs != 0 {
		t.Errorf("oversize file was indexed anyway (%d documents)", docs)
	}
}

// A read failure leaves the previous version active on purpose. The run has to
// say which documents those are, or the stale text is invisible.
func TestIndexer_NamesStaleActiveDocumentsAfterReadFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read a mode-0000 file")
	}
	database := openTestDB(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "a.md")
	if err := os.WriteFile(path, []byte("# A\n\noriginal body\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	col := config.Collection{Name: "test", Path: dir, Extensions: []string{".md"}}
	idx := New(database, 256)
	ctx := context.Background()
	if _, err := idx.Index(ctx, col); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o640) })

	_, err := idx.Index(ctx, col)
	if err == nil {
		t.Fatal("expected an error when a file could not be re-read")
	}
	if !strings.Contains(err.Error(), "still searchable: a.md") {
		t.Errorf("error does not name the stale document: %v", err)
	}

	var active int
	if err := database.QueryRowContext(ctx,
		`SELECT active FROM documents WHERE collection='test' AND path='a.md'`).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 1 {
		t.Errorf("a transient read failure must not deactivate the document, got active=%d", active)
	}
}
