package indexer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/itsmostafa/qi/internal/config"
)

func TestIndexer_RejectsEscapingFileSymlink(t *testing.T) {
	database := openTestDB(t)
	idx := New(database, 256)

	outsideDir := t.TempDir()
	secretPath := filepath.Join(outsideDir, "secret.md")
	if err := os.WriteFile(secretPath, []byte("TOKEN-OUTSIDE-COLLECTION"), 0o640); err != nil {
		t.Fatal(err)
	}

	collDir := t.TempDir()
	linkPath := filepath.Join(collDir, "innocent.md")
	if err := os.Symlink(secretPath, linkPath); err != nil {
		t.Fatal(err)
	}

	col := config.Collection{Name: "test", Path: collDir, Extensions: []string{".md"}}
	stats, err := idx.Index(context.Background(), col)
	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}
	if stats.FilesAdded != 0 {
		t.Errorf("expected 0 added, got %d", stats.FilesAdded)
	}
	if stats.FilesSkipped != 1 {
		t.Errorf("expected 1 skipped, got %d", stats.FilesSkipped)
	}

	var docCount int
	if err := database.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM documents WHERE collection='test'`).Scan(&docCount); err != nil {
		t.Fatalf("querying documents: %v", err)
	}
	if docCount != 0 {
		t.Errorf("expected no documents indexed, got %d", docCount)
	}

	var contentCount int
	if err := database.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM content`).Scan(&contentCount); err != nil {
		t.Fatalf("querying content: %v", err)
	}
	if contentCount != 0 {
		t.Errorf("expected the outside file's content to never be stored, got %d rows", contentCount)
	}
}

func TestIndexer_PurgesPreviouslyLeakedSymlinkOnReindex(t *testing.T) {
	database := openTestDB(t)
	idx := New(database, 256)
	collDir := t.TempDir()
	col := config.Collection{Name: "test", Path: collDir, Extensions: []string{".md"}}

	// Simulate a document that was indexed before this fix existed (e.g. from
	// an escaping symlink that used to be accepted).
	if _, err := database.ExecContext(context.Background(),
		`INSERT INTO content(hash, body) VALUES ('deadbeef', 'leaked secret')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(context.Background(),
		`INSERT INTO documents(collection, path, title, content_hash, active, indexed_at, updated_at)
		 VALUES ('test', 'innocent.md', 'innocent.md', 'deadbeef', 1, datetime('now'), datetime('now'))`); err != nil {
		t.Fatal(err)
	}

	// Reindex an empty collection — the stale row must be deactivated since
	// nothing in the current walk (correctly) produces that path, and compact
	// then drops the row and the leaked body with it.
	stats, err := idx.Index(context.Background(), col)
	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}
	if stats.FilesRemoved != 1 {
		t.Errorf("expected 1 removed, got %d", stats.FilesRemoved)
	}

	var docs int
	if err := database.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM documents WHERE collection='test' AND path='innocent.md'`).Scan(&docs); err != nil {
		t.Fatalf("querying document: %v", err)
	}
	if docs != 0 {
		t.Errorf("expected leaked document row to be gone, got %d", docs)
	}

	var leaked int
	if err := database.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM content WHERE hash='deadbeef'`).Scan(&leaked); err != nil {
		t.Fatalf("querying content: %v", err)
	}
	if leaked != 0 {
		t.Errorf("leaked body survived the reindex (%d rows)", leaked)
	}
}

func TestIndexer_RejectsInCollectionSymlink(t *testing.T) {
	database := openTestDB(t)
	idx := New(database, 256)

	collDir := t.TempDir()
	realPath := filepath.Join(collDir, "real.md")
	if err := os.WriteFile(realPath, []byte("# Real\nActual content."), 0o640); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(collDir, "alias.md")
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Fatal(err)
	}

	col := config.Collection{Name: "test", Path: collDir, Extensions: []string{".md"}}
	stats, err := idx.Index(context.Background(), col)
	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}
	if stats.FilesAdded != 1 {
		t.Errorf("expected only real.md to be added, got %d", stats.FilesAdded)
	}
	if stats.FilesSkipped != 1 {
		t.Errorf("expected the alias to be skipped, got %d", stats.FilesSkipped)
	}
}

func TestIndexer_SkipsDanglingSymlink(t *testing.T) {
	database := openTestDB(t)
	idx := New(database, 256)

	collDir := t.TempDir()
	missingTarget := filepath.Join(collDir, "gone.md")
	linkPath := filepath.Join(collDir, "dangling.md")
	if err := os.Symlink(missingTarget, linkPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(collDir, "ok.md"), []byte("# OK\nFine."), 0o640); err != nil {
		t.Fatal(err)
	}

	col := config.Collection{Name: "test", Path: collDir, Extensions: []string{".md"}}
	stats, err := idx.Index(context.Background(), col)
	if err != nil {
		t.Fatalf("Index failed (walk must not fail on a dangling symlink): %v", err)
	}
	if stats.FilesSkipped != 1 {
		t.Errorf("expected 1 skipped, got %d", stats.FilesSkipped)
	}
	if stats.FilesAdded != 1 {
		t.Errorf("expected 1 added (ok.md), got %d", stats.FilesAdded)
	}
}

func TestIndexer_SymlinkedCollectionRootIndexesFiles(t *testing.T) {
	database := openTestDB(t)
	idx := New(database, 256)

	realDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(realDir, "a.md"), []byte("# A\nContent."), 0o640); err != nil {
		t.Fatal(err)
	}

	parent := t.TempDir()
	linkRoot := filepath.Join(parent, "collection-link")
	if err := os.Symlink(realDir, linkRoot); err != nil {
		t.Fatal(err)
	}

	col := config.Collection{Name: "test", Path: linkRoot, Extensions: []string{".md"}}
	stats, err := idx.Index(context.Background(), col)
	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}
	if stats.FilesScanned != 1 || stats.FilesAdded != 1 {
		t.Errorf("expected the symlinked root to be walked, got scanned=%d added=%d", stats.FilesScanned, stats.FilesAdded)
	}
}

func TestIndexer_IgnoreListedRootNameIsNotWiped(t *testing.T) {
	database := openTestDB(t)
	idx := New(database, 256)

	parent := t.TempDir()
	// "dist" is in defaultIgnoreDirs — a collection rooted there must not be
	// treated as an ignored subdirectory of itself.
	collDir := filepath.Join(parent, "dist")
	if err := os.Mkdir(collDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(collDir, "a.md"), []byte("# A\nContent."), 0o640); err != nil {
		t.Fatal(err)
	}

	col := config.Collection{Name: "test", Path: collDir, Extensions: []string{".md"}}
	stats, err := idx.Index(context.Background(), col)
	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}
	if stats.FilesScanned != 1 || stats.FilesAdded != 1 {
		t.Errorf("expected the ignore-listed root name to still be walked, got scanned=%d added=%d", stats.FilesScanned, stats.FilesAdded)
	}
}

func TestIndexer_DotNamedRootIsNotWiped(t *testing.T) {
	database := openTestDB(t)
	idx := New(database, 256)

	parent := t.TempDir()
	collDir := filepath.Join(parent, ".notes")
	if err := os.Mkdir(collDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(collDir, "a.md"), []byte("# A\nContent."), 0o640); err != nil {
		t.Fatal(err)
	}

	col := config.Collection{Name: "test", Path: collDir, Extensions: []string{".md"}}
	stats, err := idx.Index(context.Background(), col)
	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}
	if stats.FilesScanned != 1 || stats.FilesAdded != 1 {
		t.Errorf("expected the dot-named root to still be walked, got scanned=%d added=%d", stats.FilesScanned, stats.FilesAdded)
	}
}

func TestIndexer_RelPathUsesCanonicalRoot(t *testing.T) {
	database := openTestDB(t)
	idx := New(database, 256)

	collDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(collDir, "a.md"), []byte("# A\nContent."), 0o640); err != nil {
		t.Fatal(err)
	}

	col := config.Collection{Name: "test", Path: collDir, Extensions: []string{".md"}}
	if _, err := idx.Index(context.Background(), col); err != nil {
		t.Fatal(err)
	}

	var path string
	if err := database.QueryRowContext(context.Background(),
		`SELECT path FROM documents WHERE collection='test'`).Scan(&path); err != nil {
		t.Fatalf("querying document: %v", err)
	}
	if path != "a.md" {
		t.Errorf("expected relative path %q, got %q (Rel must use the canonicalized root, not the raw configured path)", "a.md", path)
	}
}

// isCaseInsensitiveFS reports whether the filesystem backing dir treats
// paths differing only in letter case as the same file (the macOS/Windows
// default; typically false on Linux).
func isCaseInsensitiveFS(t *testing.T, dir string) bool {
	t.Helper()
	lower := filepath.Join(dir, "case-probe.txt")
	if err := os.WriteFile(lower, []byte("x"), 0o640); err != nil {
		t.Fatal(err)
	}
	upper := filepath.Join(dir, "CASE-PROBE.TXT")
	_, err := os.Stat(upper)
	return err == nil
}

// Even an in-collection target is rejected: consistently not following links
// is what makes the descriptor-relative open race-free.
func TestIndexer_RejectsInCollectionSymlinkWithDifferentCaseTarget(t *testing.T) {
	database := openTestDB(t)
	idx := New(database, 256)

	collDir := t.TempDir()
	if !isCaseInsensitiveFS(t, collDir) {
		t.Skip("filesystem is case-sensitive; this scenario does not apply")
	}

	subDir := filepath.Join(collDir, "sub")
	if err := os.Mkdir(subDir, 0o750); err != nil {
		t.Fatal(err)
	}
	realPath := filepath.Join(subDir, "real.md")
	if err := os.WriteFile(realPath, []byte("# Real\nContent."), 0o640); err != nil {
		t.Fatal(err)
	}

	// EvalSymlinks resolves collDir itself before we ever compare against
	// it, so build the differently-cased target from *that* resolved form
	// rather than the raw TempDir path.
	canonicalColl, err := config.CanonicalPath(collDir)
	if err != nil {
		t.Fatalf("canonicalizing: %v", err)
	}
	differentCaseTarget := strings.ToUpper(filepath.Join(canonicalColl, "sub", "real.md"))
	linkPath := filepath.Join(collDir, "alias.md")
	if err := os.Symlink(differentCaseTarget, linkPath); err != nil {
		t.Fatal(err)
	}

	col := config.Collection{Name: "test", Path: collDir, Extensions: []string{".md"}}
	stats, err := idx.Index(context.Background(), col)
	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}
	if stats.FilesSkipped != 1 {
		t.Errorf("expected the symlink to be skipped, got %d", stats.FilesSkipped)
	}
	if stats.FilesAdded != 1 {
		t.Errorf("expected only real.md to be indexed, got %d", stats.FilesAdded)
	}
}

func TestIndexer_RejectsFileReplacedBySymlinkBeforeOpen(t *testing.T) {
	database := openTestDB(t)
	idx := New(database, 256)
	collDir := t.TempDir()
	path := filepath.Join(collDir, "note.md")
	if err := os.WriteFile(path, []byte("safe"), 0o640); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret.md")
	if err := os.WriteFile(outside, []byte("TOCTOU-SECRET"), 0o640); err != nil {
		t.Fatal(err)
	}
	idx.beforeRead = func(string) {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, path); err != nil {
			t.Fatal(err)
		}
	}

	stats, err := idx.Index(context.Background(), config.Collection{Name: "test", Path: collDir, Extensions: []string{".md"}})
	if err == nil {
		t.Fatal("expected replacement race to be reported")
	}
	if stats.FilesAdded != 0 {
		t.Fatalf("replacement target was indexed: %+v", stats)
	}
	var leaked int
	if err := database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM content WHERE body = 'TOCTOU-SECRET'`).Scan(&leaked); err != nil {
		t.Fatal(err)
	}
	if leaked != 0 {
		t.Fatal("outside symlink target leaked into the index")
	}
	var recordedErr string
	if err := database.QueryRowContext(context.Background(), `SELECT error FROM index_runs ORDER BY id DESC LIMIT 1`).Scan(&recordedErr); err != nil {
		t.Fatal(err)
	}
	if recordedErr == "" {
		t.Fatal("per-file failure was not recorded on the index run")
	}
}
