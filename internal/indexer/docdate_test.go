package indexer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A document's date drives --since/--until. Frontmatter wins when it has a
// readable date; otherwise the file's mtime stands in, because a NULL would
// make the document invisible to every date filter.
func TestIndexer_DocumentDateFallsBackToModTime(t *testing.T) {
	database := openTestDB(t)
	idx := New(database, 256)
	col := makeTestCollection(t, map[string]string{
		"plain.md":   "# Plain\nNo frontmatter here.",
		"created.md": "---\ntitle: C\ncreated: 2026-07-17\n---\n\nBody.",
		"dated.md":   "---\ntitle: D\ndate: 2026-03-02\n---\n\nBody.",
	})

	mtime := time.Date(2026, 8, 9, 23, 30, 0, 0, time.Local)
	if err := os.Chtimes(filepath.Join(col.Path, "plain.md"), mtime, mtime); err != nil {
		t.Fatal(err)
	}

	if _, err := idx.Index(context.Background(), col); err != nil {
		t.Fatalf("Index failed: %v", err)
	}

	want := map[string]string{
		"plain.md":   "2026-08-09",
		"created.md": "2026-07-17",
		"dated.md":   "2026-03-02",
	}
	for path, expect := range want {
		var got string
		if err := database.QueryRowContext(context.Background(),
			`SELECT COALESCE(doc_timestamp,'') FROM documents WHERE path=?`, path).Scan(&got); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if got != expect {
			t.Errorf("%s: doc_timestamp = %q, want %q", path, got, expect)
		}
	}
}

// Rows indexed before dates existed hold a NULL timestamp, and unchanged
// content never reaches the write path, so a plain reindex must backfill them.
// It must do so without re-chunking: re-embedding a whole legacy corpus is what
// --force is for, and it needs an embedder that may not be running.
func TestIndexer_BackfillsNullDocumentDateWithoutReindexing(t *testing.T) {
	database := openTestDB(t)
	idx := New(database, 256)
	col := makeTestCollection(t, map[string]string{"a.md": "# A\nBody."})

	if _, err := idx.Index(context.Background(), col); err != nil {
		t.Fatalf("Index failed: %v", err)
	}
	if _, err := database.ExecContext(context.Background(),
		`UPDATE documents SET doc_timestamp=NULL`); err != nil {
		t.Fatal(err)
	}

	mtime := time.Date(2026, 5, 4, 12, 0, 0, 0, time.Local)
	if err := os.Chtimes(filepath.Join(col.Path, "a.md"), mtime, mtime); err != nil {
		t.Fatal(err)
	}

	stats, err := idx.Index(context.Background(), col)
	if err != nil {
		t.Fatalf("reindex failed: %v", err)
	}
	if stats.FilesAdded != 0 || stats.FilesUpdated != 0 {
		t.Errorf("backfill rewrote the document: added=%d updated=%d, want 0 and 0",
			stats.FilesAdded, stats.FilesUpdated)
	}

	var got string
	if err := database.QueryRowContext(context.Background(),
		`SELECT COALESCE(doc_timestamp,'') FROM documents WHERE path='a.md'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "2026-05-04" {
		t.Errorf("doc_timestamp = %q, want %q", got, "2026-05-04")
	}
}

// A restored file whose row never had a date must be rebuilt rather than
// reactivated: reactivation skips the write path, leaving a document no date
// filter can match.
func TestIndexer_DoesNotReactivateDatelessDocument(t *testing.T) {
	database := openTestDB(t)
	idx := New(database, 256)
	col := makeTestCollection(t, map[string]string{"a.md": "# A\nBody."})

	if _, err := idx.Index(context.Background(), col); err != nil {
		t.Fatalf("Index failed: %v", err)
	}
	if _, err := database.ExecContext(context.Background(),
		`UPDATE documents SET doc_timestamp=NULL, active=0`); err != nil {
		t.Fatal(err)
	}
	if _, err := idx.Index(context.Background(), col); err != nil {
		t.Fatalf("reindex failed: %v", err)
	}

	var got string
	var active int
	if err := database.QueryRowContext(context.Background(),
		`SELECT COALESCE(doc_timestamp,''), active FROM documents WHERE path='a.md'`).
		Scan(&got, &active); err != nil {
		t.Fatal(err)
	}
	if active != 1 {
		t.Fatalf("document not restored, active=%d", active)
	}
	if got == "" {
		t.Error("restored document still has no date")
	}
}

// A fallback date is the file's mtime, so it has to follow the mtime. When an
// undated file is touched, synced or checked out its bytes stay identical, and
// the unchanged fast-path would otherwise keep the old date forever — leaving
// --since, --until and --sort date disagreeing with the filesystem. An explicit
// frontmatter date outranks the mtime and must not move with it.
func TestIndexer_RefreshesFallbackDateWhenMtimeMoves(t *testing.T) {
	database := openTestDB(t)
	idx := New(database, 256)
	col := makeTestCollection(t, map[string]string{
		"plain.md": "# Plain\nNo frontmatter.",
		"dated.md": "---\ndate: 2026-03-02\n---\n\nBody.",
	})

	touch := func(when time.Time) {
		t.Helper()
		for _, name := range []string{"plain.md", "dated.md"} {
			if err := os.Chtimes(filepath.Join(col.Path, name), when, when); err != nil {
				t.Fatal(err)
			}
		}
	}

	touch(time.Date(2026, 5, 4, 12, 0, 0, 0, time.Local))
	if _, err := idx.Index(context.Background(), col); err != nil {
		t.Fatalf("Index failed: %v", err)
	}

	touch(time.Date(2026, 9, 1, 9, 0, 0, 0, time.Local))
	stats, err := idx.Index(context.Background(), col)
	if err != nil {
		t.Fatalf("reindex failed: %v", err)
	}
	// Refreshing a date is a metadata write: it must not report the corpus as
	// rewritten, nor re-chunk and re-embed it.
	if stats.FilesAdded != 0 || stats.FilesUpdated != 0 {
		t.Errorf("date refresh rewrote documents: added=%d updated=%d, want 0 and 0",
			stats.FilesAdded, stats.FilesUpdated)
	}

	want := map[string]string{
		"plain.md": "2026-09-01",
		"dated.md": "2026-03-02",
	}
	for path, expect := range want {
		var got string
		if err := database.QueryRowContext(context.Background(),
			`SELECT COALESCE(doc_timestamp,'') FROM documents WHERE path=?`, path).Scan(&got); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if got != expect {
			t.Errorf("%s: doc_timestamp = %q, want %q", path, got, expect)
		}
	}
}
