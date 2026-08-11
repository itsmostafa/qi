package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/itsmostafa/qi/internal/config"
)

func newRawEmbeddingServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestEmbeddingProvider_RejectsNegativeIndex(t *testing.T) {
	srv := newRawEmbeddingServer(t, `{"data":[{"embedding":[1,2,3,4],"index":-1}]}`)
	cfg := &config.EmbeddingProviderConfig{BaseURL: srv.URL, Model: "m", Dimension: 4}
	p := NewEmbedding(cfg)

	_, err := p.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected an error for a negative response index, got nil (must not panic either)")
	}
}

func TestEmbeddingProvider_RejectsOutOfRangeIndex(t *testing.T) {
	srv := newRawEmbeddingServer(t, `{"data":[{"embedding":[1,2,3,4],"index":5}]}`)
	cfg := &config.EmbeddingProviderConfig{BaseURL: srv.URL, Model: "m", Dimension: 4}
	p := NewEmbedding(cfg)

	_, err := p.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected an error for an out-of-range response index")
	}
}

func TestEmbeddingProvider_RejectsDuplicateIndex(t *testing.T) {
	srv := newRawEmbeddingServer(t, `{"data":[
		{"embedding":[1,2,3,4],"index":0},
		{"embedding":[5,6,7,8],"index":0}
	]}`)
	cfg := &config.EmbeddingProviderConfig{BaseURL: srv.URL, Model: "m", Dimension: 4}
	p := NewEmbedding(cfg)

	_, err := p.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected an error for a duplicate response index")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("expected error to mention duplicate index, got: %v", err)
	}
}

func TestEmbeddingProvider_RejectsWrongDimension(t *testing.T) {
	// Configured for dimension 2 but the endpoint returns 3 elements.
	srv := newRawEmbeddingServer(t, `{"data":[{"embedding":[1,2,3],"index":0}]}`)
	cfg := &config.EmbeddingProviderConfig{BaseURL: srv.URL, Model: "m", Dimension: 2}
	p := NewEmbedding(cfg)

	_, err := p.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected an error when the returned vector length does not match the configured dimension")
	}
}

func TestEmbeddingProviderRejectsZeroNormVector(t *testing.T) {
	srv := newRawEmbeddingServer(t, `{"data":[{"embedding":[0,0],"index":0}]}`)
	p := NewEmbedding(&config.EmbeddingProviderConfig{BaseURL: srv.URL, Model: "m", Dimension: 2})
	if _, err := p.Embed(context.Background(), []string{"hello"}); err == nil || !strings.Contains(err.Error(), "zero norm") {
		t.Fatalf("expected zero-norm error, got %v", err)
	}
}

func TestEmbeddingProviderRejectsMalformedNonFiniteVector(t *testing.T) {
	srv := newRawEmbeddingServer(t, `{"data":[{"embedding":[1e999,1],"index":0}]}`)
	p := NewEmbedding(&config.EmbeddingProviderConfig{BaseURL: srv.URL, Model: "m", Dimension: 2})
	if _, err := p.Embed(context.Background(), []string{"hello"}); err == nil {
		t.Fatal("expected non-finite/overflow JSON number to be rejected")
	}
}

func TestEmbeddingProviderRejectsNonPositiveDimension(t *testing.T) {
	p := NewEmbedding(&config.EmbeddingProviderConfig{BaseURL: "http://unused", Model: "m", Dimension: 0})
	if _, err := p.Embed(context.Background(), []string{"hello"}); err == nil {
		t.Fatal("expected non-positive dimension to be rejected before request")
	}
}

// Sanity: valid responses still marshal fine via the normal embeddingResponse
// path (guards against a typo turning json field names invalid).
func TestEmbeddingProvider_ValidResponseStillMarshals(t *testing.T) {
	var resp embeddingResponse
	if err := json.Unmarshal([]byte(`{"data":[{"embedding":[1,2],"index":0}]}`), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].Index != 0 {
		t.Fatalf("unexpected decode: %+v", resp)
	}
}
