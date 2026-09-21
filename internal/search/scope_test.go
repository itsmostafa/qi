package search

import (
	"context"
	"strings"
	"testing"

	"github.com/itsmostafa/qi/internal/db"
)

// seedScopeData gives the out-of-scope documents the stronger lexical match, so
// the in-scope one only survives if the path filter runs while candidates are
// still being collected rather than after the limit is applied.
func seedScopeData(t *testing.T, database *db.DB) {
	t.Helper()
	_, err := database.ExecContext(context.Background(), `
		INSERT INTO content(hash, body) VALUES ('h1', 'b1'), ('h2', 'b2'), ('h3', 'b3');
		INSERT INTO documents(collection, path, title, content_hash) VALUES
			('test', 'archive/2019/retro.md', 'Old Retro', 'h1'),
			('test', 'archive/2020/retro.md', 'Older Retro', 'h2'),
			('test', 'meetings/q3/retro.md', 'Current Retro', 'h3');
		INSERT INTO chunks(content_hash, doc_id, seq, text, heading_path, ordinal, content_length, start_line, end_line) VALUES
			('h1', 1, 0, 'retro retro retro retro notes', 'Intro', 0, 29, 1, 1),
			('h2', 2, 0, 'retro retro retro notes', 'Intro', 0, 23, 1, 1),
			('h3', 3, 0, 'retro notes about the quarter', 'Intro', 0, 29, 1, 1);
	`)
	if err != nil {
		t.Fatalf("seeding scope data: %v", err)
	}
}

func TestBM25_PathScopeSurvivesHigherRankedOutOfScopeHits(t *testing.T) {
	database := openTestDB(t)
	seedScopeData(t, database)

	opts := SearchOpts{Query: "retro notes", TopK: 1, Path: "meetings/*"}
	results, err := NewBM25(database).Search(context.Background(), opts)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	results = Finalize(results, opts)
	if len(results) != 1 {
		t.Fatalf("expected the in-scope document, got %d results", len(results))
	}
	if results[0].Path != "meetings/q3/retro.md" {
		t.Errorf("expected meetings/q3/retro.md, got %s", results[0].Path)
	}
}

func TestBM25_PathScopeExcludesEverythingOutside(t *testing.T) {
	database := openTestDB(t)
	seedScopeData(t, database)

	results, err := NewBM25(database).Search(context.Background(), SearchOpts{
		Query: "retro notes",
		TopK:  10,
		Path:  "archive",
	})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected both archived documents, got %d", len(results))
	}
	for _, r := range results {
		if !strings.HasPrefix(r.Path, "archive/") {
			t.Errorf("unexpected out-of-scope result %s", r.Path)
		}
	}
}
