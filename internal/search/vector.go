package search

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"sort"

	"github.com/itsmostafa/qi/internal/db"
)

// VectorSearch performs KNN search using pure Go cosine similarity.
// Embeddings are loaded from the DB and compared in memory.
// For large corpora, a dedicated vector index (sqlite-vec, etc.) is preferred.
type VectorSearch struct {
	db *db.DB
}

func NewVectorSearch(database *db.DB) *VectorSearch {
	return &VectorSearch{db: database}
}

type vecCandidate struct {
	Result
	dist float64
}

// Search returns up to topK results nearest to the query embedding.
func (v *VectorSearch) Search(ctx context.Context, queryEmbedding []float32, topK int, collection string) ([]Result, error) {
	if topK <= 0 {
		topK = 10
	}

	var collectionFilter string
	var args []any
	if collection != "" {
		collectionFilter = "AND d.collection = ?"
		args = append(args, collection)
	}

	query := fmt.Sprintf(`
		SELECT
			d.id,
			c.id,
			d.collection,
			d.path,
			COALESCE(d.title, d.path),
			COALESCE(c.heading_path, ''),
			c.text,
			cv.vector
		FROM chunk_vectors cv
		JOIN chunks c ON c.id = cv.chunk_id
		JOIN documents d ON d.id = c.doc_id
		WHERE d.active = 1
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
			&r.Title, &r.HeadingPath, &r.Snippet, &blob,
		); err != nil {
			return nil, err
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

	if topK < len(candidates) {
		candidates = candidates[:topK]
	}

	results := make([]Result, len(candidates))
	for i, c := range candidates {
		r := c.Result
		r.Score = 1.0 / (1.0 + c.dist)
		results[i] = r
	}
	return results, nil
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
