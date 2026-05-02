package indexer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/itsmostafa/qi/internal/config"
	"github.com/itsmostafa/qi/internal/db"
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
