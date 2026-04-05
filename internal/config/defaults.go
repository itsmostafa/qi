package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var nonAlphanumRe = regexp.MustCompile(`[^a-z0-9]+`)

// SlugFromPath returns a URL-safe slug derived from the last component of path.
func SlugFromPath(path string) string {
	base := filepath.Base(path)
	slug := strings.ToLower(base)
	slug = nonAlphanumRe.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "collection"
	}
	return slug
}

func DefaultConfigPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "qi", "config.yaml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "qi", "config.yaml")
}

func DefaultDBPath() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "qi", "qi.db")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "qi", "qi.db")
}

func DefaultConfig() *Config {
	return &Config{
		DatabasePath: DefaultDBPath(),
		Search: SearchConfig{
			DefaultMode: "hybrid",
			BM25TopK:    50,
			VectorTopK:  50,
			RerankTopK:  10,
			RRFK:        60,
			ChunkSize:   512,
			ChunkOverlap: 64,
		},
	}
}

var DefaultConfigTemplate = `# qi configuration
# https://github.com/itsmostafa/qi

database_path: ~/.local/share/qi/qi.db

collections:
  - name: notes
    path: ~/notes
    description: Personal notes and documents
    extensions: [.md, .txt]
    ignore: [.git, node_modules]

providers:
  # Uncomment to enable embeddings (llama.cpp / Ollama compatible)
  # embedding:
  #   name: ollama
  #   base_url: http://localhost:11434
  #   model: nomic-embed-text
  #   dimension: 768
  #   batch_size: 32

  # Or use OpenAI embeddings — set OPENAI_API_KEY in your environment
  # embedding:
  #   name: openai
  #   model: text-embedding-3-small
  #   dimension: 1536

  # Uncomment to enable LLM generation (llama.cpp / Ollama compatible)
  # generation:
  #   name: ollama
  #   base_url: http://localhost:11434
  #   model: llama3.2

  # Or use OpenAI generation — set OPENAI_API_KEY in your environment
  # generation:
  #   name: openai
  #   model: gpt-4o-mini

search:
  default_mode: hybrid   # lexical | hybrid | deep
  bm25_top_k: 50
  vector_top_k: 50
  rerank_top_k: 10
  rrf_k: 60
  chunk_size: 512
  chunk_overlap: 64
`
