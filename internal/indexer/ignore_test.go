package indexer

import (
	"context"
	"path/filepath"
	"testing"
)

func TestIndexer_IgnorePatternsCoverFilesAndGlobs(t *testing.T) {
	col := makeTestCollection(t, map[string]string{
		"keep.md":             "kept document",
		"draft.tmp.md":        "glob-ignored file",
		"README.md":           "literally ignored file",
		"archive/2020/old.md": "document under a glob-ignored directory",
		"notes/live.md":       "kept nested document",
	})
	col.Extensions = []string{".md"}
	col.Ignore = []string{"*.tmp.md", "README.md", "archive"}

	idx := New(openTestDB(t), 256)
	ctx := context.Background()
	if _, err := idx.Index(ctx, col); err != nil {
		t.Fatalf("Index failed: %v", err)
	}

	indexed := map[string]bool{}
	rows, err := idx.db.QueryContext(ctx, `SELECT path FROM documents WHERE active = 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			t.Fatal(err)
		}
		indexed[filepath.ToSlash(path)] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	want := map[string]bool{"keep.md": true, "notes/live.md": true}
	for path := range want {
		if !indexed[path] {
			t.Errorf("expected %s to be indexed", path)
		}
	}
	for path := range indexed {
		if !want[path] {
			t.Errorf("expected %s to be ignored", path)
		}
	}
}

func TestIndexer_NewIgnorePatternDeactivatesIndexedFile(t *testing.T) {
	col := makeTestCollection(t, map[string]string{
		"a.md":     "# Doc A\nContent of document A.",
		"notes.md": "Notes worth keeping.",
	})
	idx := New(openTestDB(t), 256)
	ctx := context.Background()
	if _, err := idx.Index(ctx, col); err != nil {
		t.Fatalf("first index: %v", err)
	}

	col.Ignore = []string{"a.*"}
	stats, err := idx.Index(ctx, col)
	if err != nil {
		t.Fatalf("second index: %v", err)
	}
	if stats.FilesRemoved != 1 {
		t.Fatalf("expected the newly ignored file to be removed, got %d", stats.FilesRemoved)
	}

	var count int
	if err := idx.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM documents WHERE path = 'a.md'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("expected the ignored document to be gone, found %d rows", count)
	}
}
