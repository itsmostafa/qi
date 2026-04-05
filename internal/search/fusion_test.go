package search

import (
	"testing"
)

func makeResult(chunkID int64, score float64) Result {
	return Result{ChunkID: chunkID, DocID: chunkID, Score: score}
}

func TestRRF_EmptyLists(t *testing.T) {
	result := ReciprocalRankFusion(nil, nil, 60)
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d items", len(result))
	}
}

func TestRRF_OnlyBM25(t *testing.T) {
	bm25 := []Result{
		makeResult(1, 3.0),
		makeResult(2, 2.0),
		makeResult(3, 1.0),
	}
	results := ReciprocalRankFusion(bm25, nil, 60)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	// Top result should be chunk 1 (highest BM25 rank → smallest rank number)
	if results[0].ChunkID != 1 {
		t.Errorf("expected chunk 1 first, got %d", results[0].ChunkID)
	}
}

func TestRRF_MergesLists(t *testing.T) {
	bm25 := []Result{makeResult(1, 3.0), makeResult(2, 2.0)}
	vec := []Result{makeResult(2, 0.9), makeResult(3, 0.8)}

	results := ReciprocalRankFusion(bm25, vec, 60)

	// chunk 2 appears in both → should score highest
	if results[0].ChunkID != 2 {
		t.Errorf("expected chunk 2 first (appears in both lists), got %d", results[0].ChunkID)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 unique chunks, got %d", len(results))
	}
}

func TestRRF_ScoresDescending(t *testing.T) {
	bm25 := []Result{makeResult(1, 3.0), makeResult(2, 2.0), makeResult(3, 1.0)}
	vec := []Result{makeResult(3, 0.9), makeResult(2, 0.8), makeResult(1, 0.7)}

	results := ReciprocalRankFusion(bm25, vec, 60)
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results not sorted descending at index %d: %.6f > %.6f",
				i, results[i].Score, results[i-1].Score)
		}
	}
}

func TestRRF_ArithmeticK60(t *testing.T) {
	// Rank 1 in both lists with k=60 should give 2 * 1/(60+1)
	bm25 := []Result{makeResult(1, 1.0)}
	vec := []Result{makeResult(1, 1.0)}
	results := ReciprocalRankFusion(bm25, vec, 60)
	expected := 2.0 / 61.0
	if len(results) == 0 {
		t.Fatal("no results")
	}
	got := results[0].Score
	if got < expected-0.0001 || got > expected+0.0001 {
		t.Errorf("expected RRF score ~%.6f, got %.6f", expected, got)
	}
}
