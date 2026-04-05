package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/itsmostafa/qi/internal/config"
)

type embeddingProvider struct {
	cfg    *config.EmbeddingProviderConfig
	client *http.Client
}

// NewEmbedding creates an EmbeddingProvider for an OpenAI-compatible /v1/embeddings endpoint.
func NewEmbedding(cfg *config.EmbeddingProviderConfig) EmbeddingProvider {
	return &embeddingProvider{
		cfg:    cfg,
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *embeddingProvider) Dimension() int    { return p.cfg.Dimension }
func (p *embeddingProvider) ModelName() string { return p.cfg.Model }

type embeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (p *embeddingProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	batchSize := p.cfg.BatchSize
	if batchSize <= 0 {
		batchSize = 32
	}

	var all [][]float32
	for i := 0; i < len(texts); i += batchSize {
		end := i + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[i:end]

		embeddings, err := p.embedBatch(ctx, batch)
		if err != nil {
			return nil, err
		}
		all = append(all, embeddings...)
	}
	return all, nil
}

func (p *embeddingProvider) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	body, err := json.Marshal(embeddingRequest{
		Model: p.cfg.Model,
		Input: texts,
	})
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(p.cfg.BaseURL, "/") + "/v1/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request: %w", err)
	}
	defer resp.Body.Close()

	var result embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding embedding response: %w", err)
	}

	if result.Error != nil {
		return nil, fmt.Errorf("embedding API error: %s", result.Error.Message)
	}

	// Sort by index to preserve order
	embeddings := make([][]float32, len(texts))
	for _, d := range result.Data {
		if d.Index < len(embeddings) {
			embeddings[d.Index] = d.Embedding
		}
	}

	for i, e := range embeddings {
		if e == nil {
			return nil, fmt.Errorf("missing embedding for index %d", i)
		}
	}

	return embeddings, nil
}
