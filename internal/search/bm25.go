package search

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

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
	if ftsQuery == "" {
		// Normalization yielded no usable terms (punctuation-only, emoji-only,
		// or entirely whitespace input). An empty MATCH expression is an
		// FTS5 syntax error, so return no results rather than touching FTS.
		return nil, nil
	}
	results, err := b.searchFTS(ctx, opts, ftsQuery)
	if err != nil {
		return nil, err
	}
	if len(results) > 0 {
		return results, nil
	}

	coreQuery := sanitizeFTSQueryWithoutDirectives(opts.Query)
	if coreQuery != "" && coreQuery != ftsQuery {
		results, err = b.searchFTS(ctx, opts, coreQuery)
		if err != nil {
			return nil, err
		}
		if len(results) > 0 {
			return results, nil
		}
	}

	relaxedQuery := sanitizeFTSQueryAny(opts.Query)
	if relaxedQuery == "" || relaxedQuery == ftsQuery {
		return results, nil
	}
	return b.searchFTS(ctx, opts, relaxedQuery)
}

func (b *BM25) searchFTS(ctx context.Context, opts SearchOpts, ftsQuery string) ([]Result, error) {
	var args []any
	var filters string
	if opts.Collection != "" {
		filters = "AND d.collection = ?"
		args = append(args, opts.Collection)
	}
	dateFilter, dateArgs := dateFilterSQL("d", opts)
	filters += dateFilter
	args = append(args, dateArgs...)

	// No SQL LIMIT: it would bound chunks, and one verbose file whose chunks
	// fill the pool would starve every other match once the per-document
	// collapse downstream keeps only its best chunk. Rows arrive in rank order
	// and the scan below stops at poolSize distinct documents instead.
	//
	// No snippet() either. The sort materializes every matching row before the
	// first one arrives, and a snippet per match was most of the cost of a
	// common term: 5.4s for "err" (412k matching chunks of 2.5M). Snippets are
	// fetched afterwards for the selected chunks only.
	query := fmt.Sprintf(`
		SELECT
			d.id,
			c.id,
			d.collection,
			d.path,
			d.content_hash,
			COALESCE(d.title, d.path),
			COALESCE(c.heading_path, ''),
			COALESCE(c.start_line, 0),
			COALESCE(c.end_line, 0),
			COALESCE(d.doc_timestamp, ''),
			-bm25(chunks_fts)
		FROM chunks_fts
		JOIN chunks c ON c.id = chunks_fts.rowid
		JOIN documents d ON d.id = c.doc_id
		WHERE chunks_fts MATCH ?
		  AND d.active = 1
		  AND c.start_line >= 1 AND c.end_line >= c.start_line
		  %s
		ORDER BY bm25(chunks_fts)
	`, filters)

	queryArgs := []any{ftsQuery}
	queryArgs = append(queryArgs, args...)

	// One snapshot for ranking, snippets and passages, as in vector search:
	// a concurrent `qi index` must not change the corpus between them.
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("bm25 transaction: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("bm25 query: %w", err)
	}
	defer rows.Close()

	var results []Result
	seenDoc := map[int64]bool{}
	pool := poolSize(opts)
	rank := 1
	for rows.Next() {
		var r Result
		var score float64
		if err := rows.Scan(
			&r.DocID, &r.ChunkID, &r.Collection, &r.Path, &r.Hash,
			&r.Title, &r.HeadingPath, &r.StartLine, &r.EndLine,
			&r.Timestamp, &score,
		); err != nil {
			return nil, err
		}
		if !inScope(opts.Path, r.Path) {
			continue
		}
		r.SourceURI = SourceURI(r.Collection, r.Path)
		if seenDoc[r.DocID] {
			continue // a document is represented by its best-ranked chunk
		}
		seenDoc[r.DocID] = true
		r.Score = score
		r.Scale = ScaleBM25
		if opts.Explain {
			r.Explain = &ScoreExplain{BM25Score: score, BM25Rank: rank}
		}
		results = append(results, r)
		rank++
		if len(results) >= pool {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	if err := addSnippets(ctx, tx, ftsQuery, results); err != nil {
		return nil, err
	}
	if passageLimit(opts) > 0 {
		if err := b.addPassages(ctx, tx, opts, ftsQuery, results); err != nil {
			return nil, err
		}
	}
	return results, nil
}

// idsJSON encodes ids as a JSON array for json_each, which keeps an unbounded
// result count clear of SQLite's bind-variable cap.
func idsJSON(ids []int64) string {
	b, _ := json.Marshal(ids)
	return string(b)
}

// addSnippets fills in the highlighted snippet of each result's chunk.
func addSnippets(ctx context.Context, tx *sql.Tx, ftsQuery string, results []Result) error {
	byChunk := make(map[int64]int, len(results))
	ids := make([]int64, len(results))
	for i, r := range results {
		byChunk[r.ChunkID] = i
		ids[i] = r.ChunkID
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT rowid, snippet(chunks_fts, 0, ?, ?, '...', 32)
		FROM chunks_fts
		WHERE chunks_fts MATCH ? AND rowid IN (SELECT value FROM json_each(?))`,
		HighlightOpen, HighlightClose, ftsQuery, idsJSON(ids))
	if err != nil {
		return fmt.Errorf("bm25 snippets query: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var chunkID int64
		var snippet string
		if err := rows.Scan(&chunkID, &snippet); err != nil {
			return err
		}
		results[byChunk[chunkID]].Snippet = snippet
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return rows.Close()
}

func (b *BM25) addPassages(ctx context.Context, tx *sql.Tx, opts SearchOpts, ftsQuery string, results []Result) error {
	limit := passageLimit(opts)
	if limit == 0 {
		return nil
	}
	byDoc := make(map[int64]int, len(results))
	docIDs := make([]int64, len(results))
	for i, r := range results {
		byDoc[r.DocID] = i
		docIDs[i] = r.DocID
	}
	query := `
		SELECT c.doc_id, c.id, COALESCE(c.heading_path, ''),
		       COALESCE(c.start_line, 0), COALESCE(c.end_line, 0),
		       snippet(chunks_fts, 0, ?, ?, '...', 32)
		FROM chunks_fts
		JOIN chunks c ON c.id = chunks_fts.rowid
		JOIN documents d ON d.id = c.doc_id
		WHERE chunks_fts MATCH ? AND d.active = 1
		  AND c.start_line >= 1 AND c.end_line >= c.start_line
		  AND c.doc_id IN (SELECT value FROM json_each(?))
		ORDER BY bm25(chunks_fts)
	`
	args := []any{HighlightOpen, HighlightClose, ftsQuery, idsJSON(docIDs)}
	counts := make(map[int64]int, len(results))
	seen := make(map[int64]map[int64]bool, len(results))
	for _, r := range results {
		seen[r.DocID] = map[int64]bool{r.ChunkID: true}
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("bm25 passages query: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var docID, chunkID int64
		var p Passage
		if err := rows.Scan(&docID, &chunkID, &p.HeadingPath, &p.StartLine, &p.EndLine, &p.Snippet); err != nil {
			return err
		}
		if counts[docID] >= limit || seen[docID][chunkID] {
			continue
		}
		r := &results[byDoc[docID]]
		p.ChunkID = chunkID
		r.Passages = append(r.Passages, p)
		seen[docID][chunkID] = true
		counts[docID]++
	}
	return rows.Err()
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

var ftsDirectiveWords = map[string]bool{
	"answer": true, "answers": true, "response": true, "respond": true,
	"sentence": true, "sentences": true, "paragraph": true, "paragraphs": true,
	"brief": true, "briefly": true, "concise": true, "concisely": true,
	"short": true, "shortly": true, "summarize": true, "summary": true,
	"one": true, "two": true, "three": true,
}

// sanitizeFTSQuery builds a safe FTS5 query from a natural-language string.
// It strips punctuation, filters stop words, and quotes each remaining term.
// If all terms are stop words, falls back to quoting all non-empty terms.
func sanitizeFTSQuery(q string) string {
	chosen := ftsQueryTerms(q)

	quoted := make([]string, 0, len(chosen))
	for _, t := range chosen {
		t = strings.ReplaceAll(t, `"`, `""`)
		quoted = append(quoted, `"`+t+`"`)
	}
	return strings.Join(quoted, " ")
}

func sanitizeFTSQueryWithoutDirectives(q string) string {
	chosen := ftsQueryTermsWithoutDirectives(q)

	quoted := make([]string, 0, len(chosen))
	for _, t := range chosen {
		t = strings.ReplaceAll(t, `"`, `""`)
		quoted = append(quoted, `"`+t+`"`)
	}
	return strings.Join(quoted, " ")
}

func sanitizeFTSQueryAny(q string) string {
	chosen := ftsQueryTermsWithoutDirectives(q)

	quoted := make([]string, 0, len(chosen))
	for _, t := range chosen {
		t = strings.ReplaceAll(t, `"`, `""`)
		quoted = append(quoted, `"`+t+`"`)
	}
	return strings.Join(quoted, " OR ")
}

func ftsQueryTerms(q string) []string {
	return ftsQueryTermsFiltered(q, false)
}

func ftsQueryTermsWithoutDirectives(q string) []string {
	return ftsQueryTermsFiltered(q, true)
}

func ftsQueryTermsFiltered(q string, dropDirectives bool) []string {
	terms := strings.Fields(q)

	// Keep letters, digits, and combining marks from any script — not just
	// ASCII — so CJK, Cyrillic, Arabic, and accented Latin terms survive
	// instead of collapsing to "". IsMark matters for decomposed diacritics
	// (e.g. "e" + combining acute accent).
	stripPunct := func(t string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) {
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
		lower := strings.ToLower(c)
		if !ftsStopWords[lower] && (!dropDirectives || !ftsDirectiveWords[lower]) {
			meaningful = append(meaningful, c)
		}
	}

	chosen := meaningful
	if len(chosen) == 0 {
		chosen = all
	}

	return chosen
}
