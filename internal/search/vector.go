package search

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"slices"

	"github.com/itsmostafa/qi/internal/db"
)

// VectorSearch performs KNN search using pure Go cosine similarity.
// Embeddings are loaded from the DB and compared in memory. Hybrid search
// bounds the scan to BM25's candidate documents (SearchDocs); the unbounded
// Search visits every embedded chunk and is the fallback when BM25 found none.
type VectorSearch struct {
	db          *db.DB
	fingerprint string
}

func NewVectorSearch(database *db.DB, fingerprint string) *VectorSearch {
	return &VectorSearch{db: database, fingerprint: fingerprint}
}

// vecCandidate is deliberately metadata-free: the scan visits every embedded
// chunk in the corpus, so anything held per row is paid for corpus-wide.
type vecCandidate struct {
	docID   int64
	chunkID int64
	dist    float64
}

// Search returns up to topK results nearest to the query embedding, scanning
// every embedded chunk in scope.
func (v *VectorSearch) Search(ctx context.Context, queryEmbedding []float32, topK int, opts SearchOpts) ([]Result, error) {
	return v.search(ctx, queryEmbedding, topK, opts, nil)
}

// SearchDocs is Search restricted to the chunks of docIDs. Hybrid search
// passes BM25's candidate pool, so the scan costs the candidates' chunks
// rather than the whole corpus. An empty docIDs returns nothing: it means no
// candidates, not no restriction.
func (v *VectorSearch) SearchDocs(ctx context.Context, queryEmbedding []float32, topK int, opts SearchOpts, docIDs []int64) ([]Result, error) {
	if len(docIDs) == 0 {
		return nil, nil
	}
	return v.search(ctx, queryEmbedding, topK, opts, docIDs)
}

func (v *VectorSearch) search(ctx context.Context, queryEmbedding []float32, topK int, opts SearchOpts, docIDs []int64) ([]Result, error) {
	queryNorm, err := validateVector(queryEmbedding)
	if err != nil {
		return nil, fmt.Errorf("invalid query embedding: %w", err)
	}
	if topK <= 0 {
		topK = 10
	}
	if v.fingerprint == "" {
		// No active embedding config (or, defensively, an unset fingerprint
		// that would otherwise match every pre-upgrade legacy row). Vector
		// search is meaningless without a configured embedder.
		return nil, nil
	}

	// One snapshot for both reads. Ranking and hydration are separate
	// statements, so without a transaction a concurrent `qi index` could
	// deactivate or compact a document between them and the second read
	// would answer from a database the first never saw.
	tx, err := v.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("vector search transaction: %w", err)
	}
	defer tx.Rollback()

	candidates, err := v.scanCandidates(ctx, tx, queryEmbedding, queryNorm, opts, docIDs)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(candidates, func(a, b vecCandidate) int {
		return cmp.Compare(a.dist, b.dist)
	})

	// One chunk per document, for the same reason BM25 stops at poolSize
	// distinct documents: a verbose file must not fill the pool with chunks
	// that collapse to a single result later. Supporting passages are gathered
	// only after this document pool is fixed.
	selected := make([]vecCandidate, 0, topK)
	primaryByDoc := make(map[int64]int64, topK)
	for _, c := range candidates {
		if _, seen := primaryByDoc[c.docID]; seen {
			continue
		}
		primaryByDoc[c.docID] = c.chunkID
		selected = append(selected, c)
		if len(selected) >= topK {
			break
		}
	}

	limit := passageLimit(opts)
	passagesByDoc := make(map[int64][]int64, len(selected))
	if limit > 0 {
		for _, c := range candidates {
			primary, ok := primaryByDoc[c.docID]
			if !ok || c.chunkID == primary || len(passagesByDoc[c.docID]) >= limit {
				continue
			}
			passagesByDoc[c.docID] = append(passagesByDoc[c.docID], c.chunkID)
		}
	}

	chunkIDs := make([]int64, 0, len(selected)*(1+limit))
	for _, c := range selected {
		chunkIDs = append(chunkIDs, c.chunkID)
		chunkIDs = append(chunkIDs, passagesByDoc[c.docID]...)
	}
	hydrated, err := hydrate(ctx, tx, chunkIDs)
	if err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(selected))
	for _, c := range selected {
		r, ok := hydrated[c.chunkID]
		if !ok {
			// Unreachable inside the snapshot; the map lookup still has to be
			// answered, and an absent body must not become an empty result.
			continue
		}
		r.Score = 1.0 / (1.0 + c.dist)
		for _, id := range passagesByDoc[c.docID] {
			if p, ok := hydrated[id]; ok {
				r.Passages = append(r.Passages, passageOf(p))
			}
		}
		results = append(results, r)
	}
	return results, nil
}

// scanCandidates ranks every embedded chunk in scope against the query, or
// only the chunks of docIDs when it is non-nil. Unrestricted, it is the only
// loop that runs once per chunk in the corpus, so it reads identity, the
// vector, and the path the scope glob needs -- nothing else.
func (v *VectorSearch) scanCandidates(ctx context.Context, tx *sql.Tx, queryEmbedding []float32, queryNorm float64, opts SearchOpts, docIDs []int64) ([]vecCandidate, error) {
	from := `chunk_vectors cv
		JOIN chunks c ON c.id = cv.chunk_id`
	var args []any
	if docIDs != nil {
		// Drive from the candidate list: json_each first, then chunks through
		// idx_chunks_doc_id. CROSS JOIN pins that order, so the planner cannot
		// choose to walk chunk_vectors in full and filter afterwards.
		ids, err := json.Marshal(docIDs)
		if err != nil {
			return nil, err
		}
		from = `json_each(?) cand
		CROSS JOIN chunks c ON c.doc_id = cand.value
		JOIN chunk_vectors cv ON cv.chunk_id = c.id`
		args = append(args, string(ids))
	}
	var collectionFilter string
	args = append(args, v.fingerprint)
	if opts.Collection != "" {
		collectionFilter = "AND d.collection = ?"
		args = append(args, opts.Collection)
	}
	dateFilter, dateArgs := dateFilterSQL("d", opts)
	collectionFilter += dateFilter
	args = append(args, dateArgs...)

	query := fmt.Sprintf(`
		SELECT
			d.id,
			c.id,
			d.path,
			cv.vector
		FROM %s
		JOIN documents d ON d.id = c.doc_id
		JOIN embeddings em ON em.chunk_id = cv.chunk_id
		WHERE d.active = 1
		  AND c.start_line >= 1 AND c.end_line >= c.start_line
		  AND em.fingerprint = ?
		  %s
	`, from, collectionFilter)

	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("vector search query: %w", err)
	}
	defer rows.Close()

	var candidates []vecCandidate
	for rows.Next() {
		var c vecCandidate
		var path string
		var blob []byte
		if err := rows.Scan(&c.docID, &c.chunkID, &path, &blob); err != nil {
			return nil, err
		}
		if !inScope(opts.Path, path) {
			continue
		}
		if err := db.ValidateEmbeddingBlob(blob, len(queryEmbedding)); err != nil {
			// Defense in depth: fingerprint matching should exclude stale
			// dimensions, while shared validation also rejects malformed,
			// non-finite, and zero-norm legacy vectors.
			slog.Warn("skipping invalid stored vector", "chunk_id", c.chunkID, "error", err)
			continue
		}
		c.dist = cosineDistanceBlob(queryEmbedding, queryNorm, blob)
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

// hydrate fetches the metadata and chunk text the scan left behind, keyed by
// chunk ID because the query returns rows in no particular order. The IDs
// travel as one JSON array rather than a placeholder list, so an unbounded
// caller limit cannot run into SQLite's cap on bound variables.
func hydrate(ctx context.Context, tx *sql.Tx, chunkIDs []int64) (map[int64]Result, error) {
	hydrated := make(map[int64]Result, len(chunkIDs))
	if len(chunkIDs) == 0 {
		return hydrated, nil
	}
	ids, err := json.Marshal(chunkIDs)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `
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
			c.text
		FROM chunks c
		JOIN documents d ON d.id = c.doc_id
		WHERE c.id IN (SELECT value FROM json_each(?))
	`, string(ids))
	if err != nil {
		return nil, fmt.Errorf("vector hydrate query: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var r Result
		if err := rows.Scan(
			&r.DocID, &r.ChunkID, &r.Collection, &r.Path, &r.Hash,
			&r.Title, &r.HeadingPath, &r.StartLine, &r.EndLine,
			&r.Timestamp, &r.Snippet,
		); err != nil {
			return nil, err
		}
		r.SourceURI = SourceURI(r.Collection, r.Path)
		hydrated[r.ChunkID] = r
	}
	return hydrated, rows.Err()
}

// validateVector rejects a query vector that cannot be scored against, and
// returns its norm so the scan does not recompute it once per stored vector.
func validateVector(v []float32) (float64, error) {
	if len(v) == 0 {
		return 0, fmt.Errorf("empty vector")
	}
	var norm float64
	for i, value := range v {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return 0, fmt.Errorf("non-finite value at dimension %d", i)
		}
		norm += float64(value) * float64(value)
	}
	if norm == 0 {
		return 0, fmt.Errorf("zero-norm vector")
	}
	return math.Sqrt(norm), nil
}

// cosineDistanceBlob returns 1 - cosine_similarity (range [0, 2]) against a
// stored little-endian float32 blob. It decodes and scores in one pass: the
// scan runs this once per embedded chunk in the corpus, so materializing a
// []float32 per row cost an allocation and a second walk for nothing. The
// query norm is the same for every row and is computed once by the caller.
func cosineDistanceBlob(query []float32, queryNorm float64, blob []byte) float64 {
	if queryNorm == 0 || len(blob) != len(query)*4 {
		return 2.0
	}
	var dot, norm float64
	for i, q := range query {
		stored := float64(math.Float32frombits(binary.LittleEndian.Uint32(blob[i*4:])))
		dot += float64(q) * stored
		norm += stored * stored
	}
	if norm == 0 {
		return 2.0
	}
	return 1.0 - dot/(queryNorm*math.Sqrt(norm))
}
