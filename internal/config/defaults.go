package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var nonAlphanumRe = regexp.MustCompile(`[^A-Za-z0-9]+`)

// SlugFromPath returns a stable collection name derived from a filesystem path.
func SlugFromPath(path string) string {
	parts := collectionPathParts(path)
	slug := strings.Join(parts, "-")
	slug = nonAlphanumRe.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "collection"
	}
	return slug
}

func collectionPathParts(path string) []string {
	clean := filepath.Clean(ExpandHome(path))
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rel, err := filepath.Rel(home, clean); err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
			clean = rel
		}
	}

	vol := filepath.VolumeName(clean)
	clean = strings.TrimPrefix(clean, vol)
	clean = strings.Trim(clean, string(filepath.Separator))
	if clean == "" || clean == "." {
		return nil
	}

	parts := strings.Split(clean, string(filepath.Separator))
	if len(parts) >= 3 && (parts[0] == "Users" || parts[0] == "home") {
		parts = parts[2:]
	}

	keep := parts[:0]
	for _, part := range parts {
		part = strings.Trim(nonAlphanumRe.ReplaceAllString(part, "-"), "-")
		if part != "" {
			keep = append(keep, part)
		}
	}
	return keep
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
			DefaultMode:  "hybrid",
			BM25TopK:     50,
			VectorTopK:   50,
			RerankTopK:   10,
			RRFK:         60,
			ChunkSize:    512,
			ChunkOverlap: 64,
		},
	}
}

var DefaultConfigTemplate = `# qi configuration
# https://github.com/itsmostafa/qi

database_path: ~/.local/share/qi/qi.db

collections:
  # Names are generated from paths; ~/notes becomes notes.
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
  #   max_input_chars: 24000  # safety net: truncate texts over ~6k tokens (set for 8k-token models)

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
  #   model: gpt-5.4-mini

search:
  default_mode: hybrid   # lexical | hybrid | deep
  bm25_top_k: 50
  vector_top_k: 50
  rerank_top_k: 10
  rrf_k: 60
  chunk_size: 512
  chunk_overlap: 64
  # prefer_extensions: [.md, .txt]  # boost scores for these file types
  # extension_boost: 2.0            # multiplier applied to preferred extensions (default 2.0)
`
