package search

import (
	"cmp"
	"slices"
	"strings"
)

// Score scales. BM25 magnitudes are raw and corpus-dependent; RRF scores are
// bounded near 1/rrf_k. They share a JSON key, so each result names its own.
const (
	ScaleBM25 = "bm25"
	ScaleRRF  = "rrf"
)

// Highlight markers wrap matched terms in FTS5 snippets. They are sentinels,
// not markup: the output layer turns them into ANSI or strips them. Using <b>
// here leaked HTML into JSON and into the terminal.
const (
	HighlightOpen  = "\x02"
	HighlightClose = "\x03"
)

// dateFilterSQL builds the doc_timestamp predicates for Since/Until. alias is
// the documents table alias in the caller's query.
func dateFilterSQL(alias string, opts SearchOpts) (string, []any) {
	var sql string
	var args []any
	if opts.Since != "" {
		sql += " AND " + alias + ".doc_timestamp >= ?"
		args = append(args, opts.Since)
	}
	if opts.Until != "" {
		sql += " AND " + alias + ".doc_timestamp <= ?"
		args = append(args, opts.Until)
	}
	return sql, args
}

// capResults collapses duplicate snippets and keeps one result per document.
// Boilerplate repeated verbatim across files produces byte-identical snippets,
// which is what the first pass removes.
//
// Both retrievers and the fusion already emit one chunk per document, so the
// per-document pass is a backstop rather than load-bearing: it keeps Finalize's
// contract true if a future retriever forgets.
func capResults(results []Result) []Result {
	seen := make(map[string]bool, len(results))
	seenDoc := make(map[int64]bool, len(results))
	out := results[:0:0]
	for _, r := range results {
		key := strings.TrimSpace(r.Snippet)
		if (key != "" && seen[key]) || seenDoc[r.DocID] {
			continue
		}
		seen[key] = true
		seenDoc[r.DocID] = true
		out = append(out, r)
	}
	return out
}

// sortByDate orders results newest first, falling back to score. An empty
// timestamp sorts lowest, so undated documents land last rather than leading a
// date query.
func sortByDate(results []Result) {
	slices.SortStableFunc(results, func(a, b Result) int {
		if c := cmp.Compare(b.Timestamp, a.Timestamp); c != 0 {
			return c
		}
		return cmp.Compare(b.Score, a.Score)
	})
}

// Finalize applies the ordering and trimming every command needs after
// retrieval: date sort over the whole candidate pool (never after truncation,
// or "newest" would only mean "newest of the most relevant"), duplicate and
// per-document capping, then the caller's limit.
func Finalize(results []Result, opts SearchOpts) []Result {
	if opts.Sort == "date" {
		sortByDate(results)
	}
	results = capResults(results)
	if opts.TopK > 0 && len(results) > opts.TopK {
		results = results[:opts.TopK]
	}
	return results
}

// poolSize is how many candidates a retriever should fetch before dedupe and
// capping trim the list down to TopK. Never fewer than TopK: a configured pool
// smaller than the caller's limit would silently cap the result count.
func poolSize(opts SearchOpts) int {
	n := max(opts.Pool, opts.TopK)
	if n <= 0 {
		return 10
	}
	return n
}
