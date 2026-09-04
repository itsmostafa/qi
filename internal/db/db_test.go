package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"
)

func TestRenameCollectionDataMergesDuplicateDocuments(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "qi.db"))
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	defer database.Close()

	// The same file indexed under both names: identical bytes, identical hash.
	insertRenameTestDocument(t, database, "old", "same.md", "shared body")
	insertRenameTestDocument(t, database, "old", "old.md", "old only")
	insertRenameTestDocument(t, database, "new", "same.md", "shared body")
	if _, err := database.ExecContext(ctx, `INSERT INTO index_runs(collection) VALUES ('old')`); err != nil {
		t.Fatalf("inserting index run: %v", err)
	}

	if err := database.RenameCollectionData(ctx, "old", "new"); err != nil {
		t.Fatalf("renaming collection data: %v", err)
	}

	assertDBCount(t, database, `SELECT COUNT(*) FROM documents WHERE collection = 'old'`, 0)
	assertDBCount(t, database, `SELECT COUNT(*) FROM documents WHERE collection = 'new'`, 2)
	assertDBCount(t, database, `SELECT COUNT(*) FROM documents WHERE collection = 'new' AND path = 'same.md'`, 1)
	assertDBCount(t, database, `SELECT COUNT(*) FROM chunks`, 2)
	assertDBCount(t, database, `SELECT COUNT(*) FROM embeddings`, 2)
	assertDBCount(t, database, `SELECT COUNT(*) FROM chunk_vectors`, 2)
	assertDBCount(t, database, `SELECT COUNT(*) FROM index_runs WHERE collection = 'old'`, 0)
	assertDBCount(t, database, `SELECT COUNT(*) FROM index_runs WHERE collection = 'new'`, 1)
	assertDBCount(t, database, `SELECT COUNT(*) FROM content`, 2)
}

func insertRenameTestDocument(t *testing.T, database *DB, collection, path, body string) {
	t.Helper()
	ctx := context.Background()
	sum := sha256.Sum256([]byte(body))
	hash := hex.EncodeToString(sum[:])
	if _, err := database.ExecContext(ctx,
		`INSERT OR IGNORE INTO content(hash, body) VALUES (?, ?)`,
		hash, []byte(body)); err != nil {
		t.Fatalf("inserting content: %v", err)
	}
	result, err := database.ExecContext(ctx, `
		INSERT INTO documents(collection, path, title, content_hash)
		VALUES (?, ?, ?, ?)
	`, collection, path, path, hash)
	if err != nil {
		t.Fatalf("inserting document: %v", err)
	}
	docID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("reading document id: %v", err)
	}
	result, err = database.ExecContext(ctx, `
		INSERT INTO chunks(content_hash, doc_id, seq, text, content_length)
		VALUES (?, ?, 0, ?, ?)
	`, hash, docID, body, len(body))
	if err != nil {
		t.Fatalf("inserting chunk: %v", err)
	}
	chunkID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("reading chunk id: %v", err)
	}
	if _, err := database.ExecContext(ctx,
		`INSERT INTO chunk_vectors(chunk_id, vector) VALUES (?, ?)`,
		chunkID, []byte{0, 0, 0, 0}); err != nil {
		t.Fatalf("inserting chunk vector: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO embeddings(chunk_id, provider, model, dimension)
		VALUES (?, 'test', 'test-model', 1)
	`, chunkID); err != nil {
		t.Fatalf("inserting embedding: %v", err)
	}
}

func assertDBCount(t *testing.T, database *DB, query string, want int) {
	t.Helper()
	var got int
	if err := database.QueryRowContext(context.Background(), query).Scan(&got); err != nil {
		t.Fatalf("querying count: %v", err)
	}
	if got != want {
		t.Fatalf("unexpected count for %q: got %d, want %d", query, got, want)
	}
}

// Basename-derived collection names make it easy for a rename target to be
// occupied by a different directory. Matching duplicates on path alone deleted
// the user's real documents; only genuinely identical files may be dropped.
func TestRenameCollectionDataKeepsDifferentFilesSharingAPath(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "qi.db"))
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	defer database.Close()

	insertRenameTestDocument(t, database, "old", "shared.md", "the real document")
	insertRenameTestDocument(t, database, "new", "shared.md", "an unrelated file")

	if err := database.RenameCollectionData(ctx, "old", "new"); err != nil {
		t.Fatalf("renaming collection data: %v", err)
	}

	assertDBCount(t, database, `SELECT COUNT(*) FROM documents`, 2)
	assertDBCount(t, database,
		`SELECT COUNT(*) FROM content WHERE CAST(body AS TEXT) = 'the real document'`, 1)
	// It could not move without overwriting a different file, so it stays put
	// rather than being deleted.
	assertDBCount(t, database, `SELECT COUNT(*) FROM documents WHERE collection = 'old'`, 1)
}

// One collection's old name is another's new name. Renaming in place would
// merge the chain's documents into whichever name was still occupied.
func TestRenameCollectionsHandlesChainedNames(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "qi.db"))
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	defer database.Close()

	insertRenameTestDocument(t, database, "y-x-foo", "a.md", "from y")
	insertRenameTestDocument(t, database, "x-foo", "b.md", "from x")

	if err := database.RenameCollections(ctx, [][2]string{
		{"y-x-foo", "x-foo"},
		{"x-foo", "foo"},
	}); err != nil {
		t.Fatalf("renaming collections: %v", err)
	}

	assertDBCount(t, database, `SELECT COUNT(*) FROM documents WHERE collection = 'x-foo' AND path = 'a.md'`, 1)
	assertDBCount(t, database, `SELECT COUNT(*) FROM documents WHERE collection = 'x-foo'`, 1)
	assertDBCount(t, database, `SELECT COUNT(*) FROM documents WHERE collection = 'foo' AND path = 'b.md'`, 1)
	assertDBCount(t, database, `SELECT COUNT(*) FROM documents WHERE collection = 'foo'`, 1)
	assertDBCount(t, database, `SELECT COUNT(*) FROM documents WHERE collection = 'y-x-foo'`, 0)
}
