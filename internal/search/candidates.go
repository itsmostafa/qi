package search

import (
	"context"
	"fmt"
)

// CandidateDocs returns up to limit distinct document IDs matching any term
// of the query, best BM25 rank first. It widens the vector leg's candidate
// set when the conjunctive BM25 pool is short: a strict query that matches one
// document would otherwise leave the vector leg one document to rank. It
// computes no snippets, so it costs one FTS5 lookup and a sort of the matches.
func (b *BM25) CandidateDocs(ctx context.Context, opts SearchOpts, limit int) ([]int64, error) {
	ftsQuery := sanitizeFTSQueryAny(opts.Query)
	if ftsQuery == "" || limit <= 0 {
		return nil, nil
	}
	var filters string
	args := []any{ftsQuery}
	if opts.Collection != "" {
		filters = "AND d.collection = ?"
		args = append(args, opts.Collection)
	}
	dateFilter, dateArgs := dateFilterSQL("d", opts)
	filters += dateFilter
	args = append(args, dateArgs...)

	// Same predicates as BM25's ranking query, so a candidate is a document
	// BM25 itself could have returned.
	rows, err := b.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT d.id, d.path
		FROM chunks_fts
		JOIN chunks c ON c.id = chunks_fts.rowid
		JOIN documents d ON d.id = c.doc_id
		WHERE chunks_fts MATCH ?
		  AND d.active = 1
		  AND c.start_line >= 1 AND c.end_line >= c.start_line
		  %s
		ORDER BY bm25(chunks_fts)
	`, filters), args...)
	if err != nil {
		return nil, fmt.Errorf("bm25 candidates query: %w", err)
	}
	defer rows.Close()

	var ids []int64
	seen := make(map[int64]bool)
	for rows.Next() {
		var id int64
		var path string
		if err := rows.Scan(&id, &path); err != nil {
			return nil, err
		}
		if seen[id] || !inScope(opts.Path, path) {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
		if len(ids) >= limit {
			break
		}
	}
	return ids, rows.Err()
}
