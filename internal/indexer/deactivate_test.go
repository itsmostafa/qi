package indexer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/itsmostafa/qi/internal/config"
)

type failingRows struct {
	scanErr error
	rowsErr error
	next    bool
}

func (r *failingRows) Next() bool {
	if r.next {
		r.next = false
		return true
	}
	return false
}
func (r *failingRows) Scan(...any) error { return r.scanErr }
func (r *failingRows) Err() error        { return r.rowsErr }

func TestMissingDocumentIDsReturnsScanError(t *testing.T) {
	_, err := missingDocumentIDs(&failingRows{next: true, scanErr: errors.New("scan failed")}, nil)
	if err == nil || !strings.Contains(err.Error(), "scan failed") {
		t.Fatalf("expected scan error, got %v", err)
	}
}

func TestMissingDocumentIDsReturnsRowsError(t *testing.T) {
	_, err := missingDocumentIDs(&failingRows{rowsErr: errors.New("iteration failed")}, nil)
	if err == nil || !strings.Contains(err.Error(), "iteration failed") {
		t.Fatalf("expected rows error, got %v", err)
	}
}

// A deleted file used to keep its plaintext body forever: the orphan prune in
// compact skipped it because the deactivated document still referenced the hash.
func TestIndexDropsBodyOfDeletedFile(t *testing.T) {
	database := openTestDB(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.md")
	if err := os.WriteFile(path, []byte("# S\n\nOLD-SECRET-CONTENT\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	col := config.Collection{Name: "test", Path: dir, Extensions: []string{".md"}}
	idx := New(database, 256)
	ctx := context.Background()

	if _, err := idx.Index(ctx, col); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	stats, err := idx.Index(ctx, col)
	if err != nil {
		t.Fatal(err)
	}
	if stats.FilesRemoved != 1 {
		t.Errorf("expected 1 removed, got %d", stats.FilesRemoved)
	}

	var bodies int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM content WHERE body LIKE '%OLD-SECRET-CONTENT%'`).Scan(&bodies); err != nil {
		t.Fatal(err)
	}
	if bodies != 0 {
		t.Errorf("deleted file's body survived the index run (%d rows)", bodies)
	}

	var docs int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM documents WHERE collection='test'`).Scan(&docs); err != nil {
		t.Fatal(err)
	}
	if docs != 0 {
		t.Errorf("expected the deactivated document row to be gone, got %d", docs)
	}
}
