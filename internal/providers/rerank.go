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

type rerankProvider struct {
	cfg    *config.RerankProviderConfig
	client *http.Client
}

// NewRerank creates a RerankProvider for a rerank API endpoint.
func NewRerank(cfg *config.RerankProviderConfig) RerankProvider {
	return &rerankProvider{
		cfg:    cfg,
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

func (p *rerankProvider) ModelName() string { return p.cfg.Model }

type rerankRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
}

type rerankResponse struct {
	Results []struct {
		Index          int     `json:"index"`
		RelevanceScore float64 `json:"relevance_score"`
	} `json:"results"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (p *rerankProvider) Rerank(ctx context.Context, query string, passages []string) ([]float64, error) {
	body, err := json.Marshal(rerankRequest{
		Model:     p.cfg.Model,
		Query:     query,
		Documents: passages,
	})
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(p.cfg.BaseURL, "/") + "/v1/rerank"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rerank request: %w", err)
	}
	defer resp.Body.Close()

	var result rerankResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding rerank response: %w", err)
	}

	if result.Error != nil {
		return nil, fmt.Errorf("rerank API error: %s", result.Error.Message)
	}

	scores := make([]float64, len(passages))
	for _, r := range result.Results {
		if r.Index < len(scores) {
			scores[r.Index] = r.RelevanceScore
		}
	}
	return scores, nil
}
