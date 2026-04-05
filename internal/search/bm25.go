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

// ftsStopWords are common English words excluded from FTS5 queries to avoid
// zero-result conjunctions when searching natural-language questions.
var ftsStopWords = map[string]bool{
	"a": true, "an": true, "the": true,
	"in": true, "on": true, "at": true, "to": true, "for": true, "of": true,
	"with": true, "from": true, "by": true, "as": true, "into": true, "about": true,
	"and": true, "or": true, "but": true, "nor": true,
	"what": true, "who": true, "which": true, "when": true, "where": true, "why": true, "how": true,
	"is": true, "are": true, "was": true, "were": true, "be": true, "been": true, "being": true,
	"have": true, "has": true, "had": true, "do": true, "does": true, "did": true,
	"will": true, "would": true, "could": true, "should": true, "may": true, "might": true,
	"it": true, "its": true, "this": true, "that": true, "these": true, "those": true,
	"i": true, "me": true, "my": true, "we": true, "our": true,
	"you": true, "your": true, "he": true, "she": true, "they": true, "them": true,
	"not": true, "if": true, "then": true, "so": true, "up": true, "out": true,
}

// sanitizeFTSQuery builds a safe FTS5 query from a natural-language string.
// It strips punctuation, filters stop words, and quotes each remaining term.
// If all terms are stop words, falls back to quoting all non-empty terms.
func sanitizeFTSQuery(q string) string {
	terms := strings.Fields(q)

	stripPunct := func(t string) string {
		return strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
				return r
			}
			return -1
		}, t)
	}

	var meaningful, all []string
	for _, t := range terms {
		c := stripPunct(t)
		if c == "" {
			continue
		}
		all = append(all, c)
		if !ftsStopWords[strings.ToLower(c)] {
			meaningful = append(meaningful, c)
		}
	}

	chosen := meaningful
	if len(chosen) == 0 {
		chosen = all
	}

	quoted := make([]string, 0, len(chosen))
	for _, t := range chosen {
		t = strings.ReplaceAll(t, `"`, `""`)
		quoted = append(quoted, `"`+t+`"`)
	}
	return strings.Join(quoted, " ")
}
