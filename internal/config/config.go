package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Collection struct {
	Name        string   `yaml:"name"`
	Path        string   `yaml:"path"`
	Description string   `yaml:"description,omitempty"`
	Extensions  []string `yaml:"extensions,omitempty"`
	Ignore      []string `yaml:"ignore,omitempty"`
}

type EmbeddingProviderConfig struct {
	Name      string `yaml:"name"`
	BaseURL   string `yaml:"base_url"`
	Model     string `yaml:"model"`
	Dimension int    `yaml:"dimension"`
	BatchSize int    `yaml:"batch_size,omitempty"`
}

type RerankProviderConfig struct {
	Name    string `yaml:"name"`
	BaseURL string `yaml:"base_url"`
	Model   string `yaml:"model"`
}

type GenerationProviderConfig struct {
	Name    string `yaml:"name"`
	BaseURL string `yaml:"base_url"`
	Model   string `yaml:"model"`
	APIKey  string `yaml:"api_key,omitempty"`
}

type Providers struct {
	Embedding  *EmbeddingProviderConfig  `yaml:"embedding,omitempty"`
	Rerank     *RerankProviderConfig     `yaml:"rerank,omitempty"`
	Generation *GenerationProviderConfig `yaml:"generation,omitempty"`
}

type SearchConfig struct {
	DefaultMode    string `yaml:"default_mode"`
	BM25TopK       int    `yaml:"bm25_top_k"`
	VectorTopK     int    `yaml:"vector_top_k"`
	RerankTopK     int    `yaml:"rerank_top_k"`
	RRFK           int    `yaml:"rrf_k"`
	ChunkSize      int    `yaml:"chunk_size"`
	ChunkOverlap   int    `yaml:"chunk_overlap"`
}

type Config struct {
	DatabasePath string       `yaml:"database_path"`
	Collections  []Collection `yaml:"collections"`
	Providers    Providers    `yaml:"providers"`
	Search       SearchConfig `yaml:"search"`
}

func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultConfigPath()
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving config path: %w", err)
	}
	configDir := filepath.Dir(absPath)

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", absPath, err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	cfg.expandPaths()
	cfg.resolveRelativePaths(configDir)

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

// ExpandHome expands a leading ~ to the user home directory.
func ExpandHome(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}

func (c *Config) expandPaths() {
	c.DatabasePath = ExpandHome(c.DatabasePath)
	for i := range c.Collections {
		c.Collections[i].Path = ExpandHome(c.Collections[i].Path)
	}
}

func (c *Config) resolveRelativePaths(baseDir string) {
	if c.DatabasePath != "" && !filepath.IsAbs(c.DatabasePath) {
		c.DatabasePath = filepath.Join(baseDir, c.DatabasePath)
	}
	for i := range c.Collections {
		p := c.Collections[i].Path
		if p != "" && !filepath.IsAbs(p) {
			c.Collections[i].Path = filepath.Join(baseDir, p)
		}
	}
}

func (c *Config) validate() error {
	seen := map[string]bool{}
	for _, col := range c.Collections {
		if col.Name == "" {
			return fmt.Errorf("collection missing name")
		}
		if col.Path == "" {
			return fmt.Errorf("collection %q missing path", col.Name)
		}
		if seen[col.Name] {
			return fmt.Errorf("duplicate collection name %q", col.Name)
		}
		seen[col.Name] = true
	}
	return nil
}
