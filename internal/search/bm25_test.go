package search

import (
	"context"
	"fmt"
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

func TestBM25_RelaxesNaturalLanguageQueryWhenStrictSearchMisses(t *testing.T) {
	database := openTestDB(t)
	seedTestData(t, database)

	bm25 := NewBM25(database)
	results, err := bm25.Search(context.Background(), SearchOpts{
		Query: "What is Go? Response in one sentence.",
		TopK:  10,
	})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected relaxed search to find at least one result")
	}
	if results[0].Title != "Go Programming" {
		t.Fatalf("expected Go Programming first, got %q", results[0].Title)
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

// A verbose document whose chunks fill the pool must not starve every other
// match: the pool is bounded by distinct documents, not by chunks.
func TestBM25PoolIsBoundedByDocuments(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	if _, err := database.ExecContext(ctx, `
		INSERT INTO content(hash, body) VALUES ('hv', 'verbose');
		INSERT INTO documents(collection, path, title, content_hash)
			VALUES ('test', 'verbose.md', 'Verbose', 'hv');
	`); err != nil {
		t.Fatalf("seeding verbose document: %v", err)
	}
	for i := range 30 {
		if _, err := database.ExecContext(ctx,
			`INSERT INTO chunks(content_hash, doc_id, seq, text, heading_path, ordinal, content_length)
			 VALUES ('hv', 1, ?, 'widget widget widget', 'S', 0, 20)`, i); err != nil {
			t.Fatalf("seeding chunk %d: %v", i, err)
		}
	}
	for i := range 5 {
		hash, path := fmt.Sprintf("h%d", i), fmt.Sprintf("other%d.md", i)
		if _, err := database.ExecContext(ctx,
			`INSERT INTO content(hash, body) VALUES (?, 'other')`, hash); err != nil {
			t.Fatalf("seeding other content %d: %v", i, err)
		}
		if _, err := database.ExecContext(ctx,
			`INSERT INTO documents(collection, path, title, content_hash) VALUES ('test', ?, 'Other', ?)`,
			path, hash); err != nil {
			t.Fatalf("seeding other document %d: %v", i, err)
		}
		if _, err := database.ExecContext(ctx,
			`INSERT INTO chunks(content_hash, doc_id, seq, text, heading_path, ordinal, content_length)
			 SELECT ?, id, 0, 'a widget lives here', 'S', 0, 19 FROM documents WHERE path = ?`,
			hash, path); err != nil {
			t.Fatalf("seeding chunk for other document %d: %v", i, err)
		}
	}

	results, err := NewBM25(database).Search(ctx, SearchOpts{Query: "widget", Pool: 10, TopK: 10})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	docs := map[int64]bool{}
	for _, r := range results {
		if docs[r.DocID] {
			t.Errorf("document %d returned twice", r.DocID)
		}
		docs[r.DocID] = true
	}
	if len(docs) != 6 {
		t.Errorf("got %d distinct documents, want 6: the verbose file starved the pool", len(docs))
	}
}
