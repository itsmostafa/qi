package search

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"sort"

	"github.com/itsmostafa/qi/internal/db"
)

// VectorSearch performs KNN search using pure Go cosine similarity.
// Embeddings are loaded from the DB and compared in memory.
// For large corpora, a dedicated vector index (sqlite-vec, etc.) is preferred.
type VectorSearch struct {
	db          *db.DB
	fingerprint string
}

func NewVectorSearch(database *db.DB, fingerprint string) *VectorSearch {
	return &VectorSearch{db: database, fingerprint: fingerprint}
}

type vecCandidate struct {
	Result
	dist float64
}

// Search returns up to topK results nearest to the query embedding.
func (v *VectorSearch) Search(ctx context.Context, queryEmbedding []float32, topK int, opts SearchOpts) ([]Result, error) {
	if err := validateVector(queryEmbedding); err != nil {
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

	var collectionFilter string
	args := []any{v.fingerprint}
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
			d.collection,
			d.path,
			COALESCE(d.title, d.path),
			COALESCE(c.heading_path, ''),
			COALESCE(d.doc_timestamp, ''),
			c.text,
			cv.vector
		FROM chunk_vectors cv
		JOIN chunks c ON c.id = cv.chunk_id
		JOIN documents d ON d.id = c.doc_id
		JOIN embeddings em ON em.chunk_id = cv.chunk_id
		WHERE d.active = 1
		  AND em.fingerprint = ?
		  %s
	`, collectionFilter)

	rows, err := v.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("vector search query: %w", err)
	}
	defer rows.Close()

	var candidates []vecCandidate
	for rows.Next() {
		var r Result
		var blob []byte
		if err := rows.Scan(
			&r.DocID, &r.ChunkID, &r.Collection, &r.Path,
			&r.Title, &r.HeadingPath, &r.Timestamp, &r.Snippet, &blob,
		); err != nil {
			return nil, err
		}
		if err := db.ValidateEmbeddingBlob(blob, len(queryEmbedding)); err != nil {
			// Defense in depth: fingerprint matching should exclude stale
			// dimensions, while shared validation also rejects malformed,
			// non-finite, and zero-norm legacy vectors.
			slog.Warn("skipping invalid stored vector", "chunk_id", r.ChunkID, "error", err)
			continue
		}
		vec := deserializeFloat32(blob)
		dist := cosineDistance(queryEmbedding, vec)
		candidates = append(candidates, vecCandidate{Result: r, dist: dist})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Sort by distance ascending (lower = more similar)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].dist < candidates[j].dist
	})

	// One chunk per document, for the same reason BM25 stops at poolSize
	// distinct documents: a verbose file must not fill the pool with chunks
	// that collapse to a single result later.
	deduped := candidates[:0:0]
	seenDoc := map[int64]bool{}
	for _, c := range candidates {
		if seenDoc[c.DocID] {
			continue
		}
		seenDoc[c.DocID] = true
		deduped = append(deduped, c)
		if len(deduped) >= topK {
			break
		}
	}
	candidates = deduped

	results := make([]Result, len(candidates))
	for i, c := range candidates {
		r := c.Result
		r.Score = 1.0 / (1.0 + c.dist)
		results[i] = r
	}
	return results, nil
}

func validateVector(v []float32) error {
	if len(v) == 0 {
		return fmt.Errorf("empty vector")
	}
	var norm float64
	for i, value := range v {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return fmt.Errorf("non-finite value at dimension %d", i)
		}
		norm += float64(value) * float64(value)
	}
	if norm == 0 {
		return fmt.Errorf("zero-norm vector")
	}
	return nil
}

// cosineDistance returns 1 - cosine_similarity (range [0, 2]).
func cosineDistance(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 2.0
	}
	var dot, normA, normB float64
	for i := range a {
		ai, bi := float64(a[i]), float64(b[i])
		dot += ai * bi
		normA += ai * ai
		normB += bi * bi
	}
	if normA == 0 || normB == 0 {
		return 2.0
	}
	return 1.0 - dot/(math.Sqrt(normA)*math.Sqrt(normB))
}

// deserializeFloat32 decodes little-endian bytes to float32 slice.
func deserializeFloat32(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		bits := binary.LittleEndian.Uint32(b[i*4:])
		v[i] = math.Float32frombits(bits)
	}
	return v
}
