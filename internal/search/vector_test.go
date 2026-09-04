package search

import (
	"context"
	"math"
	"testing"
)

func TestVectorSearch_FiltersByFingerprint(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	if _, err := database.ExecContext(ctx, `
		INSERT INTO content(hash, body) VALUES ('h1', 'b1');
		INSERT INTO documents(collection, path, title, content_hash) VALUES ('test', 'a.md', 'A', 'h1');
		INSERT INTO chunks(content_hash, doc_id, seq, text, heading_path, ordinal, content_length)
			VALUES ('h1', 1, 0, 'chunk from current model', 'Intro', 0, 25);

		INSERT INTO content(hash, body) VALUES ('h2', 'b2');
		INSERT INTO documents(collection, path, title, content_hash) VALUES ('test', 'b.md', 'B', 'h2');
		INSERT INTO chunks(content_hash, doc_id, seq, text, heading_path, ordinal, content_length)
			VALUES ('h2', 2, 0, 'chunk from stale model', 'Intro', 0, 23);
	`); err != nil {
		t.Fatalf("seeding documents/chunks: %v", err)
	}

	var currentChunkID, staleChunkID int64
	if err := database.QueryRowContext(ctx, `SELECT id FROM chunks WHERE doc_id=1`).Scan(&currentChunkID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT id FROM chunks WHERE doc_id=2`).Scan(&staleChunkID); err != nil {
		t.Fatal(err)
	}

	if err := database.UpsertEmbedding(ctx, currentChunkID, []float32{1, 0, 0, 0}, "test", "model-b", 4, "fp-current"); err != nil {
		t.Fatalf("upserting current embedding: %v", err)
	}
	if err := database.UpsertEmbedding(ctx, staleChunkID, []float32{1, 0, 0, 0}, "test", "model-a", 4, "fp-stale"); err != nil {
		t.Fatalf("upserting stale embedding: %v", err)
	}

	vs := NewVectorSearch(database, "fp-current")
	results, err := vs.Search(ctx, []float32{1, 0, 0, 0}, 10, SearchOpts{})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected exactly 1 result (only the current-fingerprint chunk), got %d: %+v", len(results), results)
	}
	if results[0].Path != "a.md" {
		t.Errorf("expected result from a.md (current fingerprint), got %q", results[0].Path)
	}
}

func TestVectorSearch_EmptyFingerprintReturnsNoResults(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	if _, err := database.ExecContext(ctx, `
		INSERT INTO content(hash, body) VALUES ('h1', 'b1');
		INSERT INTO documents(collection, path, title, content_hash) VALUES ('test', 'a.md', 'A', 'h1');
		INSERT INTO chunks(content_hash, doc_id, seq, text, heading_path, ordinal, content_length)
			VALUES ('h1', 1, 0, 'chunk text', 'Intro', 0, 10);
	`); err != nil {
		t.Fatal(err)
	}
	var chunkID int64
	if err := database.QueryRowContext(ctx, `SELECT id FROM chunks`).Scan(&chunkID); err != nil {
		t.Fatal(err)
	}
	// Simulate a legacy pre-upgrade row: fingerprint defaults to ''.
	if err := database.UpsertEmbedding(ctx, chunkID, []float32{1, 0, 0, 0}, "test", "model", 4, ""); err != nil {
		t.Fatal(err)
	}

	vs := NewVectorSearch(database, "")
	results, err := vs.Search(ctx, []float32{1, 0, 0, 0}, 10, SearchOpts{})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected an empty (unconfigured) active fingerprint to match nothing, got %d results", len(results))
	}
}

func TestVectorSearchRejectsZeroNormAndNonFiniteQueries(t *testing.T) {
	vs := NewVectorSearch(openTestDB(t), "fp")
	if _, err := vs.Search(context.Background(), []float32{0, 0}, 10, SearchOpts{}); err == nil {
		t.Fatal("expected zero-norm query rejection")
	}
	if _, err := vs.Search(context.Background(), []float32{1, math.Float32frombits(0x7fc00000)}, 10, SearchOpts{}); err == nil {
		t.Fatal("expected non-finite query rejection")
	}
}

func TestVectorSearch_SkipsMismatchedDimensionDefensively(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	if _, err := database.ExecContext(ctx, `
		INSERT INTO content(hash, body) VALUES ('h1', 'b1');
		INSERT INTO documents(collection, path, title, content_hash) VALUES ('test', 'a.md', 'A', 'h1');
		INSERT INTO chunks(content_hash, doc_id, seq, text, heading_path, ordinal, content_length)
			VALUES ('h1', 1, 0, 'chunk text', 'Intro', 0, 10);
	`); err != nil {
		t.Fatal(err)
	}
	var chunkID int64
	if err := database.QueryRowContext(ctx, `SELECT id FROM chunks`).Scan(&chunkID); err != nil {
		t.Fatal(err)
	}
	// Same fingerprint, but a shorter vector than the query — should never
	// happen after the fingerprint join, but must not crash if it does.
	if err := database.UpsertEmbedding(ctx, chunkID, []float32{1, 0, 0}, "test", "model", 3, "fp-x"); err != nil {
		t.Fatal(err)
	}

	vs := NewVectorSearch(database, "fp-x")
	results, err := vs.Search(ctx, []float32{1, 0, 0, 0}, 10, SearchOpts{})
	if err != nil {
		t.Fatalf("Search must not error on a dimension mismatch: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected the mismatched-dimension vector to be skipped, got %d results", len(results))
	}
}

// A document with several near chunks must not occupy several slots: the pool
// is bounded by documents, so its nearest chunk represents it.
func TestVectorSearch_OneChunkPerDocument(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	if _, err := database.ExecContext(ctx, `
		INSERT INTO content(hash, body) VALUES ('h1', 'b1');
		INSERT INTO documents(collection, path, title, content_hash) VALUES ('test', 'a.md', 'A', 'h1');
		INSERT INTO chunks(content_hash, doc_id, seq, text, heading_path, ordinal, content_length)
			VALUES ('h1', 1, 0, 'near', 'Intro', 0, 4);
		INSERT INTO chunks(content_hash, doc_id, seq, text, heading_path, ordinal, content_length)
			VALUES ('h1', 1, 1, 'also near', 'Intro', 0, 9);

		INSERT INTO content(hash, body) VALUES ('h2', 'b2');
		INSERT INTO documents(collection, path, title, content_hash) VALUES ('test', 'b.md', 'B', 'h2');
		INSERT INTO chunks(content_hash, doc_id, seq, text, heading_path, ordinal, content_length)
			VALUES ('h2', 2, 0, 'further', 'Intro', 0, 7);
	`); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	// Both of doc A's chunks are nearer the query than doc B's single chunk.
	vecs := map[int64][]float32{}
	rows, err := database.QueryContext(ctx, `SELECT id, doc_id, seq FROM chunks ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id, docID int64
		var seq int
		if err := rows.Scan(&id, &docID, &seq); err != nil {
			t.Fatal(err)
		}
		switch {
		case docID == 1 && seq == 0:
			vecs[id] = []float32{1, 0, 0, 0}
		case docID == 1:
			vecs[id] = []float32{0.99, 0.14, 0, 0}
		default:
			vecs[id] = []float32{0, 1, 0, 0}
		}
	}
	rows.Close()
	for id, v := range vecs {
		if err := database.UpsertEmbedding(ctx, id, v, "test", "m", 4, "fp"); err != nil {
			t.Fatalf("upserting embedding for chunk %d: %v", id, err)
		}
	}

	results, err := NewVectorSearch(database, "fp").Search(ctx, []float32{1, 0, 0, 0}, 2, SearchOpts{})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2 (one per document): %+v", len(results), results)
	}
	if results[0].Path != "a.md" || results[1].Path != "b.md" {
		t.Errorf("got %q then %q, want a.md then b.md — doc A took both slots", results[0].Path, results[1].Path)
	}
}
