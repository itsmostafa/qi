package search

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/itsmostafa/qi/internal/db"
)

// LLMCache caches LLM responses keyed by SHA-256(model + prompt).
type LLMCache struct {
	db *db.DB
}

func NewLLMCache(database *db.DB) *LLMCache {
	return &LLMCache{db: database}
}

func cacheKey(model, prompt string) string {
	h := sha256.Sum256([]byte(model + "\x00" + prompt))
	return hex.EncodeToString(h[:])
}

// Get returns a cached response, or ("", false) if not found.
func (c *LLMCache) Get(ctx context.Context, model, prompt string) (string, bool) {
	key := cacheKey(model, prompt)
	var response string
	row := c.db.QueryRowContext(ctx,
		`SELECT response FROM llm_cache WHERE key = ?`, key)
	if err := row.Scan(&response); err != nil {
		return "", false
	}
	return response, true
}

// Set stores a response in the cache.
func (c *LLMCache) Set(ctx context.Context, model, prompt, response string) error {
	key := cacheKey(model, prompt)
	_, err := c.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO llm_cache(key, model, response) VALUES (?, ?, ?)`,
		key, model, response)
	if err != nil {
		return fmt.Errorf("caching llm response: %w", err)
	}
	return nil
}
