package providers

import "context"

// EmbeddingProvider generates vector embeddings for text.
type EmbeddingProvider interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Dimension() int
	ModelName() string
}

// RerankProvider scores candidate passages for relevance to a query.
type RerankProvider interface {
	Rerank(ctx context.Context, query string, passages []string) ([]float64, error)
	ModelName() string
}

// GenerationProvider generates text completions.
type GenerationProvider interface {
	Complete(ctx context.Context, req CompletionRequest) (string, error)
	ModelName() string
}

// CompletionRequest is a chat completion request.
type CompletionRequest struct {
	SystemPrompt string
	UserPrompt   string
	MaxTokens    int
}
