package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/itsmostafa/qi/internal/config"
)

func TestEmbeddingProvider_Embed(t *testing.T) {
	// Mock /v1/embeddings server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var req embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// Return fake embeddings
		resp := embeddingResponse{}
		for i := range req.Input {
			vec := make([]float32, 4)
			vec[i%4] = 1.0
			resp.Data = append(resp.Data, struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{Embedding: vec, Index: i})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	cfg := &config.EmbeddingProviderConfig{
		BaseURL:   srv.URL,
		Model:     "test-model",
		Dimension: 4,
		BatchSize: 10,
	}
	p := NewEmbedding(cfg)

	texts := []string{"hello", "world", "foo"}
	embeddings, err := p.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if len(embeddings) != len(texts) {
		t.Errorf("expected %d embeddings, got %d", len(texts), len(embeddings))
	}
	for i, e := range embeddings {
		if len(e) != 4 {
			t.Errorf("embedding[%d] has wrong dimension: %d", i, len(e))
		}
	}
}

func TestEmbeddingProvider_Dimension(t *testing.T) {
	cfg := &config.EmbeddingProviderConfig{Dimension: 768, Model: "m", BaseURL: "http://localhost"}
	p := NewEmbedding(cfg)
	if p.Dimension() != 768 {
		t.Errorf("expected 768, got %d", p.Dimension())
	}
}

func TestEmbeddingProvider_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(embeddingResponse{
			Error: &struct {
				Message string `json:"message"`
			}{Message: "model not found"},
		})
	}))
	defer srv.Close()

	cfg := &config.EmbeddingProviderConfig{BaseURL: srv.URL, Model: "bad", Dimension: 4}
	p := NewEmbedding(cfg)
	_, err := p.Embed(context.Background(), []string{"test"})
	if err == nil {
		t.Error("expected error for API error response")
	}
}
