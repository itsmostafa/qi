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
	results, err := vs.Search(ctx, []float32{1, 0, 0, 0}, 10, "")
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
	results, err := vs.Search(ctx, []float32{1, 0, 0, 0}, 10, "")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected an empty (unconfigured) active fingerprint to match nothing, got %d results", len(results))
	}
}

func TestVectorSearchRejectsZeroNormAndNonFiniteQueries(t *testing.T) {
	vs := NewVectorSearch(openTestDB(t), "fp")
	if _, err := vs.Search(context.Background(), []float32{0, 0}, 10, ""); err == nil {
		t.Fatal("expected zero-norm query rejection")
	}
	if _, err := vs.Search(context.Background(), []float32{1, math.Float32frombits(0x7fc00000)}, 10, ""); err == nil {
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
	results, err := vs.Search(ctx, []float32{1, 0, 0, 0}, 10, "")
	if err != nil {
		t.Fatalf("Search must not error on a dimension mismatch: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected the mismatched-dimension vector to be skipped, got %d results", len(results))
	}
}
