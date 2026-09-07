package search

import (
	"testing"
)

func makeResult(chunkID int64, score float64) Result {
	return Result{ChunkID: chunkID, DocID: chunkID, Score: score}
}

func makeResultPath(chunkID int64, score float64, path string) Result {
	return Result{ChunkID: chunkID, DocID: chunkID, Score: score, Path: path}
}

func TestApplyExtensionBoost_PreferredMovesUp(t *testing.T) {
	results := []Result{
		makeResultPath(1, 1.0, "file.go"),
		makeResultPath(2, 0.9, "README.md"),
		makeResultPath(3, 0.8, "notes.txt"),
	}
	boosted := applyExtensionBoost(results, []string{".md", ".txt"}, 2.0)
	if boosted[0].ChunkID != 2 {
		t.Errorf("expected README.md first after boost, got chunk %d", boosted[0].ChunkID)
	}
	if boosted[1].ChunkID != 3 {
		t.Errorf("expected notes.txt second, got chunk %d", boosted[1].ChunkID)
	}
}

func TestApplyExtensionBoost_NoExts(t *testing.T) {
	results := []Result{makeResultPath(1, 1.0, "a.go"), makeResultPath(2, 0.5, "b.md")}
	out := applyExtensionBoost(results, nil, 2.0)
	if out[0].ChunkID != 1 {
		t.Errorf("order should be unchanged with no preferred exts")
	}
}

func TestApplyExtensionBoost_DefaultBoost(t *testing.T) {
	results := []Result{
		makeResultPath(1, 1.0, "file.go"),
		makeResultPath(2, 0.9, "doc.md"),
	}
	// boost=0 should use default 2.0
	boosted := applyExtensionBoost(results, []string{".md"}, 0)
	if boosted[0].ChunkID != 2 {
		t.Errorf("expected doc.md first with default 2x boost, got chunk %d", boosted[0].ChunkID)
	}
}

func TestRRF_EmptyLists(t *testing.T) {
	result := ReciprocalRankFusion(nil, nil, 60, 0)
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
	results := ReciprocalRankFusion(bm25, nil, 60, 0)
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

	results := ReciprocalRankFusion(bm25, vec, 60, 0)

	// chunk 2 appears in both → should score highest
	if results[0].ChunkID != 2 {
		t.Errorf("expected chunk 2 first (appears in both lists), got %d", results[0].ChunkID)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 unique chunks, got %d", len(results))
	}
}

func TestRRF_MergesPassagesWithoutChangingWinnerOrRank(t *testing.T) {
	bm25 := []Result{{DocID: 99, ChunkID: 90, Score: 3}, {
		DocID: 1, ChunkID: 10, Hash: "bm-hash", SourceURI: "qi://c/a.md", Score: 2,
		Passages: []Passage{{ChunkID: 11, Snippet: "bm support"}, {ChunkID: 12, Snippet: "shared"}},
	}}
	vec := []Result{{
		DocID: 1, ChunkID: 20, Hash: "vec-hash", SourceURI: "qi://c/a.md", Score: 1,
		Passages: []Passage{{ChunkID: 12, Snippet: "shared"}, {ChunkID: 13, Snippet: "vec support"}},
	}}
	results := ReciprocalRankFusion(bm25, vec, 60, 3)
	var winner Result
	for _, r := range results {
		if r.DocID == 1 {
			winner = r
		}
	}
	if winner.ChunkID != 20 || winner.Hash != "vec-hash" {
		t.Fatalf("winner metadata = %+v, want vector winner", winner)
	}
	if len(winner.Passages) != 3 {
		t.Fatalf("passages = %+v, want 3 unique supports", winner.Passages)
	}
	seen := map[int64]bool{}
	for _, p := range winner.Passages {
		if seen[p.ChunkID] {
			t.Fatalf("passage chunk %d counted twice", p.ChunkID)
		}
		seen[p.ChunkID] = true
	}
	if winner.Explain == nil || winner.Explain.BM25Rank != 2 || winner.Explain.VectorRank != 1 {
		t.Fatalf("rank explanation = %+v", winner.Explain)
	}
}

func TestRRF_ScoresDescending(t *testing.T) {
	bm25 := []Result{makeResult(1, 3.0), makeResult(2, 2.0), makeResult(3, 1.0)}
	vec := []Result{makeResult(3, 0.9), makeResult(2, 0.8), makeResult(1, 0.7)}

	results := ReciprocalRankFusion(bm25, vec, 60, 0)
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
	results := ReciprocalRankFusion(bm25, vec, 60, 0)
	expected := 2.0 / 61.0
	if len(results) == 0 {
		t.Fatal("no results")
	}
	got := results[0].Score
	if got < expected-0.0001 || got > expected+0.0001 {
		t.Errorf("expected RRF score ~%.6f, got %.6f", expected, got)
	}
}

func TestRRF_FusesByDocument(t *testing.T) {
	// Doc 20 matches once at rank 1; doc 10 matches twice, at ranks 2 and 3.
	// Best-rank fusion gives doc 10 1/62, which stays behind doc 20's 1/61.
	// Summing doc 10's chunks would give 1/62+1/63 > 1/61 and flip the order.
	bm25 := []Result{
		{DocID: 20, ChunkID: 3, Score: 3.0},
		{DocID: 10, ChunkID: 1, Score: 2.0},
		{DocID: 10, ChunkID: 2, Score: 1.0},
	}
	results := ReciprocalRankFusion(bm25, nil, 60, 0)

	if len(results) != 2 {
		t.Fatalf("expected 2 results (one per document), got %d", len(results))
	}
	if results[0].DocID != 20 {
		t.Errorf("expected doc 20 first, got doc %d", results[0].DocID)
	}
	if results[1].DocID != 10 || results[1].ChunkID != 1 {
		t.Errorf("expected doc 10 represented by its best chunk 1, got doc %d chunk %d",
			results[1].DocID, results[1].ChunkID)
	}
	if want := 1.0 / 62.0; results[1].Score < want-1e-9 || results[1].Score > want+1e-9 {
		t.Errorf("doc 10 scored %.6f, want %.6f (best rank only, not summed)", results[1].Score, want)
	}
}

// The chunk shown must come from the list that ranked the document better, or
// the snippet contradicts the ranks printed beside it.
func TestRRF_RepresentativeChunkComesFromTheBetterRankedList(t *testing.T) {
	bm25 := []Result{
		{DocID: 99, ChunkID: 900},
		{DocID: 98, ChunkID: 800},
		{DocID: 10, ChunkID: 1},
	}
	vec := []Result{{DocID: 10, ChunkID: 2}}

	for _, r := range ReciprocalRankFusion(bm25, vec, 60, 0) {
		if r.DocID != 10 {
			continue
		}
		if r.ChunkID != 2 {
			t.Errorf("doc 10 represented by chunk %d, want chunk 2 (vector rank 1 beats bm25 rank 3)", r.ChunkID)
		}
		return
	}
	t.Fatal("doc 10 missing from the fused results")
}
