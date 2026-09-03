package search

import "testing"

func res(doc int64, chunk int64, snippet string, score float64, ts string) Result {
	return Result{DocID: doc, ChunkID: chunk, Snippet: snippet, Score: score, Timestamp: ts}
}

// A boilerplate banner repeated verbatim across files took four consecutive
// slots at an identical BM25 score.
func TestCapResultsCollapsesIdenticalSnippets(t *testing.T) {
	in := []Result{
		res(1, 1, "unique one", 9, ""),
		res(2, 2, "same banner", 5, ""),
		res(3, 3, "same banner", 5, ""),
		res(4, 4, "same banner", 5, ""),
		res(5, 5, "unique two", 4, ""),
	}
	got := capResults(in)
	if len(got) != 3 {
		t.Fatalf("got %d results, want 3: %+v", len(got), got)
	}
	if got[1].DocID != 2 || got[2].DocID != 5 {
		t.Errorf("wrong survivors: %+v", got)
	}
}

func TestCapResultsLimitsChunksPerDocument(t *testing.T) {
	in := []Result{
		res(1, 1, "a", 9, ""), res(1, 2, "b", 8, ""),
		res(1, 3, "c", 7, ""), res(2, 4, "d", 6, ""),
	}
	got := capResults(in)
	if len(got) != 3 {
		t.Fatalf("got %d results, want 3: %+v", len(got), got)
	}
	if got[2].DocID != 2 {
		t.Errorf("document 1 exceeded the cap: %+v", got)
	}
}

// Empty snippets are not evidence of duplication; collapsing them would drop
// every undescribed hit but one.
func TestCapResultsKeepsMultipleEmptySnippets(t *testing.T) {
	in := []Result{res(1, 1, "", 9, ""), res(2, 2, "", 8, ""), res(3, 3, "  ", 7, "")}
	if got := capResults(in); len(got) != 3 {
		t.Fatalf("got %d results, want 3: %+v", len(got), got)
	}
}

// The point of --sort date is "the newest", which means sorting the whole
// candidate pool before the limit is applied — not reordering the top N.
func TestFinalizeSortsPoolBeforeTruncating(t *testing.T) {
	in := []Result{
		res(1, 1, "a", 9, "2026-01-01"),
		res(2, 2, "b", 8, "2026-02-01"),
		res(3, 3, "c", 7, "2026-09-01"),
	}
	got := Finalize(in, SearchOpts{TopK: 1, Sort: "date"})
	if len(got) != 1 || got[0].DocID != 3 {
		t.Fatalf("got %+v, want only the 2026-09-01 document", got)
	}
}

func TestSortByDatePutsUndatedLast(t *testing.T) {
	in := []Result{res(1, 1, "a", 9, ""), res(2, 2, "b", 1, "2026-01-01")}
	sortByDate(in)
	if in[0].DocID != 2 {
		t.Errorf("undated document sorted first: %+v", in)
	}
}

func TestFinalizeRelevanceOrderIsUntouched(t *testing.T) {
	in := []Result{res(1, 1, "a", 9, "2020-01-01"), res(2, 2, "b", 8, "2026-01-01")}
	got := Finalize(in, SearchOpts{TopK: 5})
	if got[0].DocID != 1 {
		t.Errorf("relevance order changed without --sort date: %+v", got)
	}
}

func TestDateFilterSQL(t *testing.T) {
	sql, args := dateFilterSQL("d", SearchOpts{Since: "2026-01-01", Until: "2026-12-31"})
	if sql != " AND d.doc_timestamp >= ? AND d.doc_timestamp <= ?" {
		t.Errorf("sql = %q", sql)
	}
	if len(args) != 2 {
		t.Errorf("args = %v", args)
	}
	if sql, args := dateFilterSQL("d", SearchOpts{}); sql != "" || args != nil {
		t.Errorf("empty opts produced %q %v", sql, args)
	}
}

// -n above the configured bm25_top_k must still return -n results, not the
// pool size.
func TestPoolSizeNeverBelowTopK(t *testing.T) {
	if got := poolSize(SearchOpts{Pool: 50, TopK: 100}); got != 100 {
		t.Errorf("poolSize = %d, want 100", got)
	}
	if got := poolSize(SearchOpts{Pool: 50, TopK: 10}); got != 50 {
		t.Errorf("poolSize = %d, want 50", got)
	}
	if got := poolSize(SearchOpts{}); got != 10 {
		t.Errorf("poolSize = %d, want the default 10", got)
	}
}
