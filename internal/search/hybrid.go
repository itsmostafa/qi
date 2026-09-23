package search

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/itsmostafa/qi/internal/config"
	"github.com/itsmostafa/qi/internal/providers"
)

// Hybrid orchestrates BM25 + vector search + RRF fusion.
type Hybrid struct {
	bm25      *BM25
	vector    *VectorSearch
	embedding providers.EmbeddingProvider
	cfg       config.SearchConfig
}

func NewHybrid(bm25 *BM25, vector *VectorSearch, embedding providers.EmbeddingProvider, cfg config.SearchConfig) *Hybrid {
	return &Hybrid{
		bm25:      bm25,
		vector:    vector,
		embedding: embedding,
		cfg:       cfg,
	}
}

// Search runs BM25 and (optionally) vector search, then fuses with RRF.
func (h *Hybrid) Search(ctx context.Context, opts SearchOpts) ([]Result, error) {
	// BM25 is always run
	bm25Opts := opts
	bm25Opts.Pool = h.cfg.BM25TopK
	if bm25Opts.Pool <= 0 {
		bm25Opts.Pool = 50
	}

	bm25Results, err := h.bm25.Search(ctx, bm25Opts)
	if err != nil {
		return nil, fmt.Errorf("bm25 search: %w", err)
	}

	preferExts := h.cfg.PreferExtensions
	extBoost := h.cfg.ExtensionBoost

	// Strong-signal shortcut: if top BM25 score is >> #2, skip vector search
	if len(bm25Results) >= 2 && h.embedding != nil {
		topScore := bm25Results[0].Score
		secondScore := bm25Results[1].Score
		if topScore > 0 && secondScore > 0 && topScore/secondScore > 3.0 {
			slog.Debug("strong BM25 signal, skipping vector search",
				"top", topScore, "second", secondScore)
			return applyExtensionBoost(bm25Results, preferExts, extBoost), nil
		}
	}

	// No embedding provider — fall back to BM25 only
	if h.embedding == nil {
		slog.Debug("no embedding provider configured, using BM25 only")
		return applyExtensionBoost(bm25Results, preferExts, extBoost), nil
	}

	// Embed the query
	embeddings, err := h.embedding.Embed(ctx, []string{opts.Query})
	if err != nil || len(embeddings) == 0 {
		if err == nil {
			slog.Warn("embedding provider returned no vectors for query, falling back to BM25")
		} else {
			slog.Warn("embedding query failed, falling back to BM25", "error", err)
		}
		return applyExtensionBoost(bm25Results, preferExts, extBoost), nil
	}
	queryVec := embeddings[0]

	vecTopK := h.cfg.VectorTopK
	if vecTopK <= 0 {
		vecTopK = 50
	}
	// Finalize can only return what it is given: a pool smaller than the
	// caller's limit caps the result count when BM25 contributes little.
	if opts.TopK > vecTopK {
		vecTopK = opts.TopK
	}

	var vecResults []Result
	if len(bm25Results) > 0 {
		var docIDs []int64
		docIDs, err = h.vectorCandidates(ctx, opts, bm25Results, poolSize(bm25Opts), vecTopK)
		if err == nil {
			vecResults, err = h.vector.SearchDocs(ctx, queryVec, vecTopK, opts, docIDs)
		}
	} else {
		// No query term occurs in scope even after relaxation. Scanning every
		// embedded chunk is the only way to answer, and leaving a purely
		// semantic query empty would be worse than the cost.
		vecResults, err = h.vector.Search(ctx, queryVec, vecTopK, opts)
	}
	if err != nil {
		slog.Warn("vector search failed, falling back to BM25", "error", err)
		return applyExtensionBoost(bm25Results, preferExts, extBoost), nil
	}

	// RRF fusion
	k := h.cfg.RRFK
	if k <= 0 {
		k = 60
	}
	fused := ReciprocalRankFusion(bm25Results, vecResults, k, passageLimit(opts))

	if !opts.Explain {
		for i := range fused {
			fused[i].Explain = nil
		}
	}

	// Truncation happens in Finalize, after dedupe and the per-document cap:
	// cutting to TopK here would spend slots on duplicates.
	return applyExtensionBoost(fused, preferExts, extBoost), nil
}

// vectorCandidates picks the documents the vector leg may rank. Scoring every
// embedded chunk costs the whole corpus per query (1.4s at 424k chunks);
// scoring BM25's candidates costs their chunks alone. The price is that a
// document sharing no term with the query cannot surface through the vector
// leg. A full BM25 pool is used as is. A short one -- a strict conjunction
// that matched a handful of documents -- is widened with the best documents
// matching any query term, so the vector leg still has up to vecTopK
// documents to rank and a small lexical match does not cap the result count.
func (h *Hybrid) vectorCandidates(ctx context.Context, opts SearchOpts, bm25Results []Result, pool, vecTopK int) ([]int64, error) {
	ids := make([]int64, 0, max(len(bm25Results), vecTopK))
	seen := make(map[int64]bool, cap(ids))
	for _, r := range bm25Results {
		if !seen[r.DocID] {
			seen[r.DocID] = true
			ids = append(ids, r.DocID)
		}
	}
	if len(bm25Results) >= pool {
		return ids, nil
	}
	wider, err := h.bm25.CandidateDocs(ctx, opts, vecTopK)
	if err != nil {
		return nil, err
	}
	for _, id := range wider {
		if len(ids) >= vecTopK {
			break
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, nil
}
