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
	bm25Opts.TopK = h.cfg.BM25TopK
	if bm25Opts.TopK <= 0 {
		bm25Opts.TopK = 50
	}

	bm25Results, err := h.bm25.Search(ctx, bm25Opts)
	if err != nil {
		return nil, fmt.Errorf("bm25 search: %w", err)
	}

	// Strong-signal shortcut: if top BM25 score is >> #2, skip vector search
	if len(bm25Results) >= 2 && h.embedding != nil {
		topScore := bm25Results[0].Score
		secondScore := bm25Results[1].Score
		if topScore > 0 && secondScore > 0 && topScore/secondScore > 3.0 {
			slog.Debug("strong BM25 signal, skipping vector search",
				"top", topScore, "second", secondScore)
			if opts.TopK > 0 && len(bm25Results) > opts.TopK {
				bm25Results = bm25Results[:opts.TopK]
			}
			return bm25Results, nil
		}
	}

	// No embedding provider — fall back to BM25 only
	if h.embedding == nil {
		slog.Debug("no embedding provider configured, using BM25 only")
		if opts.TopK > 0 && len(bm25Results) > opts.TopK {
			bm25Results = bm25Results[:opts.TopK]
		}
		return bm25Results, nil
	}

	// Embed the query
	embeddings, err := h.embedding.Embed(ctx, []string{opts.Query})
	if err != nil {
		slog.Warn("embedding query failed, falling back to BM25", "error", err)
		if opts.TopK > 0 && len(bm25Results) > opts.TopK {
			bm25Results = bm25Results[:opts.TopK]
		}
		return bm25Results, nil
	}
	queryVec := embeddings[0]

	vecTopK := h.cfg.VectorTopK
	if vecTopK <= 0 {
		vecTopK = 50
	}

	vecResults, err := h.vector.Search(ctx, queryVec, vecTopK, opts.Collection)
	if err != nil {
		slog.Warn("vector search failed, falling back to BM25", "error", err)
		if opts.TopK > 0 && len(bm25Results) > opts.TopK {
			bm25Results = bm25Results[:opts.TopK]
		}
		return bm25Results, nil
	}

	// RRF fusion
	k := h.cfg.RRFK
	if k <= 0 {
		k = 60
	}
	fused := ReciprocalRankFusion(bm25Results, vecResults, k)

	if !opts.Explain {
		for i := range fused {
			fused[i].Explain = nil
		}
	}

	topK := opts.TopK
	if topK <= 0 {
		topK = 10
	}
	if len(fused) > topK {
		fused = fused[:topK]
	}

	return fused, nil
}
