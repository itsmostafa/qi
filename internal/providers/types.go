package providers

import (
	"context"
	"strings"
)

// apiBase normalizes a base_url so it never ends with "/v1", then appends the
// given path segment. This lets users set base_url to either the API root
// (e.g. "http://localhost:11434") or the versioned root
// (e.g. "https://api.hpc.inl.gov/llm/v1") without getting a doubled /v1 path.
func apiBase(baseURL, path string) string {
	u := strings.TrimRight(baseURL, "/")
	u = strings.TrimSuffix(u, "/v1")
	return u + path
}

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
