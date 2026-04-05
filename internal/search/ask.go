package search

import (
	"context"
	"fmt"

	"github.com/itsmostafa/qi/internal/db"
	"github.com/itsmostafa/qi/internal/providers"
)

// Asker retrieves relevant chunks and generates an LLM answer with citations.
type Asker struct {
	hybrid    *Hybrid
	bm25      *BM25
	generator providers.GenerationProvider
	cache     *LLMCache
	topK      int
}

func NewAsker(hybrid *Hybrid, bm25 *BM25, generator providers.GenerationProvider, database *db.DB, topK int) *Asker {
	return &Asker{
		hybrid:    hybrid,
		bm25:      bm25,
		generator: generator,
		cache:     NewLLMCache(database),
		topK:      topK,
	}
}

// AskResult holds the generated answer and its source chunks.
type AskResult struct {
	Answer  string
	Sources []Result
}

// Ask retrieves context and generates an answer.
func (a *Asker) Ask(ctx context.Context, question string, collection string) (*AskResult, error) {
	opts := SearchOpts{
		Query:      question,
		Collection: collection,
		TopK:       a.topK,
		Mode:       "hybrid",
	}

	var results []Result
	var err error

	if a.hybrid != nil {
		results, err = a.hybrid.Search(ctx, opts)
	} else {
		results, err = a.bm25.Search(ctx, opts)
	}
	if err != nil {
		return nil, fmt.Errorf("retrieving context: %w", err)
	}

	if len(results) == 0 {
		return &AskResult{
			Answer:  "I couldn't find any relevant information in your knowledge base to answer this question.",
			Sources: nil,
		}, nil
	}

	systemPrompt, userPrompt := BuildPrompt(question, results)
	modelName := a.generator.ModelName()

	// Check cache
	fullPrompt := systemPrompt + "\n\n" + userPrompt
	if cached, ok := a.cache.Get(ctx, modelName, fullPrompt); ok {
		return &AskResult{Answer: cached, Sources: results}, nil
	}

	answer, err := a.generator.Complete(ctx, providers.CompletionRequest{
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
	})
	if err != nil {
		return nil, fmt.Errorf("generating answer: %w", err)
	}

	// Cache the response
	_ = a.cache.Set(ctx, modelName, fullPrompt, answer)

	return &AskResult{Answer: answer, Sources: results}, nil
}
