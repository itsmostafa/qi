package search

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"

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

// vecCandidate is deliberately metadata-free: the scan visits every embedded
// chunk in the corpus, so anything held per row is paid for corpus-wide.
type vecCandidate struct {
	docID   int64
	chunkID int64
	dist    float64
}

// hydrateBatch bounds one IN-clause fetch. The caller's limit is unbounded,
// and SQLite refuses a statement carrying more variables than its maximum.
const hydrateBatch = 500

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

	// The scan selects only what ranking needs: identity, the vector, and the
	// path the scope glob is matched against. Chunk text and document metadata
	// are fetched afterwards for the handful of chunks that survive, so scan
	// cost tracks vector count rather than total corpus bytes.
	query := fmt.Sprintf(`
		SELECT
			d.id,
			c.id,
			d.path,
			cv.vector
		FROM chunk_vectors cv
		JOIN chunks c ON c.id = cv.chunk_id
		JOIN documents d ON d.id = c.doc_id
		JOIN embeddings em ON em.chunk_id = cv.chunk_id
		WHERE d.active = 1
		  AND c.start_line >= 1 AND c.end_line >= c.start_line
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
		c.dist = cosineDistance(queryEmbedding, deserializeFloat32(blob))
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// The pool holds one connection, so the scan must be finished before
	// hydration can issue its own query.
	if err := rows.Close(); err != nil {
		return nil, err
	}

	// Sort by distance ascending (lower = more similar)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].dist < candidates[j].dist
	})

	// One chunk per document, for the same reason BM25 stops at poolSize
	// distinct documents: a verbose file must not fill the pool with chunks
	// that collapse to a single result later. Supporting passages are gathered
	// only after this document pool is fixed.
	selected := make([]vecCandidate, 0, topK)
	seenDoc := map[int64]bool{}
	for _, c := range candidates {
		if seenDoc[c.docID] {
			continue
		}
		seenDoc[c.docID] = true
		selected = append(selected, c)
		if len(selected) >= topK {
			break
		}
	}

	var passages []vecCandidate
	if limit := passageLimit(opts); limit > 0 {
		primaryByDoc := make(map[int64]int64, len(selected))
		counts := make(map[int64]int, len(selected))
		for _, c := range selected {
			primaryByDoc[c.docID] = c.chunkID
		}
		for _, c := range candidates {
			primary, ok := primaryByDoc[c.docID]
			if !ok || c.chunkID == primary || counts[c.docID] >= limit {
				continue
			}
			passages = append(passages, c)
			counts[c.docID]++
		}
	}

	chunkIDs := make([]int64, 0, len(selected)+len(passages))
	for _, c := range selected {
		chunkIDs = append(chunkIDs, c.chunkID)
	}
	for _, c := range passages {
		chunkIDs = append(chunkIDs, c.chunkID)
	}
	hydrated, err := v.hydrate(ctx, chunkIDs)
	if err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(selected))
	indexByDoc := make(map[int64]int, len(selected))
	for _, c := range selected {
		r, ok := hydrated[c.chunkID]
		if !ok {
			// A concurrent `qi index` compaction hard-deleted the chunk
			// between the scan and the fetch. Drop it rather than report a
			// result with no content.
			continue
		}
		r.Score = 1.0 / (1.0 + c.dist)
		indexByDoc[c.docID] = len(results)
		results = append(results, r)
	}
	for _, c := range passages {
		r, ok := hydrated[c.chunkID]
		if !ok {
			continue
		}
		i, ok := indexByDoc[c.docID]
		if !ok {
			continue
		}
		results[i].Passages = append(results[i].Passages, Passage{
			ChunkID: r.ChunkID, HeadingPath: r.HeadingPath, Snippet: r.Snippet,
			StartLine: r.StartLine, EndLine: r.EndLine,
		})
	}
	return results, nil
}

// hydrate fetches the metadata and chunk text the scan left behind, keyed by
// chunk ID because the IN query returns rows in no particular order.
func (v *VectorSearch) hydrate(ctx context.Context, chunkIDs []int64) (map[int64]Result, error) {
	hydrated := make(map[int64]Result, len(chunkIDs))
	for start := 0; start < len(chunkIDs); start += hydrateBatch {
		batch := chunkIDs[start:min(start+hydrateBatch, len(chunkIDs))]
		placeholders := make([]string, len(batch))
		args := make([]any, len(batch))
		for i, id := range batch {
			placeholders[i] = "?"
			args[i] = id
		}
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
				c.text
			FROM chunks c
			JOIN documents d ON d.id = c.doc_id
			WHERE c.id IN (%s)
		`, strings.Join(placeholders, ","))
		if err := v.scanHydrated(ctx, query, args, hydrated); err != nil {
			return nil, err
		}
	}
	return hydrated, nil
}

func (v *VectorSearch) scanHydrated(ctx context.Context, query string, args []any, into map[int64]Result) error {
	rows, err := v.db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("vector hydrate query: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var r Result
		if err := rows.Scan(
			&r.DocID, &r.ChunkID, &r.Collection, &r.Path, &r.Hash,
			&r.Title, &r.HeadingPath, &r.StartLine, &r.EndLine,
			&r.Timestamp, &r.Snippet,
		); err != nil {
			return err
		}
		r.SourceURI = SourceURI(r.Collection, r.Path)
		into[r.ChunkID] = r
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return rows.Close()
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
