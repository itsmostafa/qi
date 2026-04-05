package search

import (
	"context"
	"fmt"
	"strings"

	"github.com/itsmostafa/qi/internal/db"
)

// BM25 runs a full-text search using SQLite FTS5's built-in BM25 ranking.
type BM25 struct {
	db *db.DB
}

func NewBM25(database *db.DB) *BM25 {
	return &BM25{db: database}
}

// Search returns up to topK results ranked by BM25.
func (b *BM25) Search(ctx context.Context, opts SearchOpts) ([]Result, error) {
	if opts.TopK <= 0 {
		opts.TopK = 10
	}

	// Escape FTS5 query: wrap each token in quotes to avoid syntax errors
	ftsQuery := sanitizeFTSQuery(opts.Query)

	var args []any
	var collectionFilter string
	if opts.Collection != "" {
		collectionFilter = "AND d.collection = ?"
		args = append(args, opts.Collection)
	}

	query := fmt.Sprintf(`
		SELECT
			d.id,
			c.id,
			d.collection,
			d.path,
			COALESCE(d.title, d.path),
			COALESCE(c.heading_path, ''),
			snippet(chunks_fts, 0, '<b>', '</b>', '...', 32),
			-bm25(chunks_fts)
		FROM chunks_fts
		JOIN chunks c ON c.id = chunks_fts.rowid
		JOIN documents d ON d.id = c.doc_id
		WHERE chunks_fts MATCH ?
		  AND d.active = 1
		  %s
		ORDER BY bm25(chunks_fts)
		LIMIT ?
	`, collectionFilter)

	queryArgs := []any{ftsQuery}
	queryArgs = append(queryArgs, args...)
	queryArgs = append(queryArgs, opts.TopK)

	rows, err := b.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("bm25 query: %w", err)
	}
	defer rows.Close()

	var results []Result
	rank := 1
	for rows.Next() {
		var r Result
		var score float64
		if err := rows.Scan(
			&r.DocID, &r.ChunkID, &r.Collection, &r.Path,
			&r.Title, &r.HeadingPath, &r.Snippet, &score,
		); err != nil {
			return nil, err
		}
		r.Score = score
		if opts.Explain {
			r.Explain = &ScoreExplain{BM25Score: score, BM25Rank: rank}
		}
		results = append(results, r)
		rank++
	}

	return results, rows.Err()
}

// sanitizeFTSQuery wraps each term in double-quotes to prevent FTS5 syntax errors.
func sanitizeFTSQuery(q string) string {
	terms := strings.Fields(q)
	quoted := make([]string, 0, len(terms))
	for _, t := range terms {
		t = strings.ReplaceAll(t, `"`, `""`)
		quoted = append(quoted, `"`+t+`"`)
	}
	return strings.Join(quoted, " ")
}
