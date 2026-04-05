package search

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/itsmostafa/qi/internal/db"
)

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func seedTestData(t *testing.T, database *db.DB) {
	t.Helper()
	ctx := context.Background()
	_, err := database.ExecContext(ctx, `
		INSERT INTO content(hash, body) VALUES ('hash1', 'body1');
		INSERT INTO documents(collection, path, title, content_hash)
			VALUES ('test', 'doc1.md', 'Go Programming', 'hash1');
		INSERT INTO chunks(content_hash, doc_id, seq, text, heading_path, ordinal, content_length)
			VALUES ('hash1', 1, 0, 'Go is an open source programming language.', 'Intro', 0, 41);

		INSERT INTO content(hash, body) VALUES ('hash2', 'body2');
		INSERT INTO documents(collection, path, title, content_hash)
			VALUES ('test', 'doc2.md', 'Python Tutorial', 'hash2');
		INSERT INTO chunks(content_hash, doc_id, seq, text, heading_path, ordinal, content_length)
			VALUES ('hash2', 2, 0, 'Python is a high-level programming language.', 'Intro', 0, 44);
	`)
	if err != nil {
		t.Fatalf("seeding test data: %v", err)
	}
}

func TestBM25_Search(t *testing.T) {
	database := openTestDB(t)
	seedTestData(t, database)

	bm25 := NewBM25(database)
	results, err := bm25.Search(context.Background(), SearchOpts{
		Query: "Go programming",
		TopK:  10,
	})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	// "Go programming" should match the Go document
	found := false
	for _, r := range results {
		if r.Title == "Go Programming" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'Go Programming' in results, got: %+v", results)
	}
}

func TestBM25_CollectionFilter(t *testing.T) {
	database := openTestDB(t)
	seedTestData(t, database)

	// Add a second collection
	ctx := context.Background()
	_, err := database.ExecContext(ctx, `
		INSERT INTO content(hash, body) VALUES ('hash3', 'body3');
		INSERT INTO documents(collection, path, title, content_hash)
			VALUES ('other', 'doc3.md', 'Rust Language', 'hash3');
		INSERT INTO chunks(content_hash, doc_id, seq, text, heading_path, ordinal, content_length)
			VALUES ('hash3', 3, 0, 'Rust is a systems programming language.', 'Intro', 0, 39);
	`)
	if err != nil {
		t.Fatalf("seeding: %v", err)
	}

	bm25 := NewBM25(database)
	results, err := bm25.Search(ctx, SearchOpts{
		Query:      "programming language",
		Collection: "test",
		TopK:       10,
	})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	for _, r := range results {
		if r.Collection != "test" {
			t.Errorf("expected collection 'test', got %q", r.Collection)
		}
	}
}

func TestBM25_ExplainPopulated(t *testing.T) {
	database := openTestDB(t)
	seedTestData(t, database)

	bm25 := NewBM25(database)
	results, err := bm25.Search(context.Background(), SearchOpts{
		Query:   "programming",
		TopK:    10,
		Explain: true,
	})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	for _, r := range results {
		if r.Explain == nil {
			t.Error("expected Explain to be populated when Explain=true")
		}
		if r.Explain != nil && r.Explain.BM25Rank <= 0 {
			t.Errorf("expected positive BM25Rank, got %d", r.Explain.BM25Rank)
		}
	}
}

// Prevent unused import
var _ = runtime.Version
