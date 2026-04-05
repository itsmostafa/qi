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

type generationProvider struct {
	cfg    *config.GenerationProviderConfig
	client *http.Client
}

// NewGeneration creates a GenerationProvider for an OpenAI-compatible /v1/chat/completions endpoint.
func NewGeneration(cfg *config.GenerationProviderConfig) GenerationProvider {
	return &generationProvider{
		cfg:    cfg,
		client: &http.Client{Timeout: 300 * time.Second},
	}
}

func (p *generationProvider) ModelName() string { return p.cfg.Model }

type chatRequest struct {
	Model     string        `json:"model"`
	Messages  []chatMessage `json:"messages"`
	MaxTokens int           `json:"max_tokens,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (p *generationProvider) Complete(ctx context.Context, req CompletionRequest) (string, error) {
	messages := []chatMessage{}
	if req.SystemPrompt != "" {
		messages = append(messages, chatMessage{Role: "system", Content: req.SystemPrompt})
	}
	messages = append(messages, chatMessage{Role: "user", Content: req.UserPrompt})

	body, err := json.Marshal(chatRequest{
		Model:     p.cfg.Model,
		Messages:  messages,
		MaxTokens: req.MaxTokens,
	})
	if err != nil {
		return "", err
	}

	url := strings.TrimRight(p.cfg.BaseURL, "/") + "/v1/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.cfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("generation request: %w", err)
	}
	defer resp.Body.Close()

	var result chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding generation response: %w", err)
	}

	if result.Error != nil {
		return "", fmt.Errorf("generation API error: %s", result.Error.Message)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no completion choices returned")
	}

	return result.Choices[0].Message.Content, nil
}
