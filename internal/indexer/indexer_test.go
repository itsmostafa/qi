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
