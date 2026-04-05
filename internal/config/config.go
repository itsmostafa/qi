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
	APIKey    string `yaml:"api_key,omitempty"`
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
	cfg.applyEnvOverrides()

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

const openAIBaseURL = "https://api.openai.com"

func (c *Config) applyEnvOverrides() {
	apiKey := os.Getenv("OPENAI_API_KEY")

	if emb := c.Providers.Embedding; emb != nil && emb.Name == "openai" {
		if emb.BaseURL == "" {
			emb.BaseURL = openAIBaseURL
		}
		if emb.APIKey == "" {
			emb.APIKey = apiKey
		}
	}

	if gen := c.Providers.Generation; gen != nil && gen.Name == "openai" {
		if gen.BaseURL == "" {
			gen.BaseURL = openAIBaseURL
		}
		if gen.APIKey == "" {
			gen.APIKey = apiKey
		}
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

// AddCollection adds or updates a named collection in the config file at configPath.
// Existing YAML comments and structure are preserved via yaml.Node round-trip.
func AddCollection(configPath string, col Collection) error {
	if configPath == "" {
		configPath = DefaultConfigPath()
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parsing config: %w", err)
	}
	if len(doc.Content) == 0 {
		return fmt.Errorf("empty config document")
	}
	root := doc.Content[0]

	// Find the collections sequence node in the root mapping.
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "collections" {
			continue
		}
		seq := root.Content[i+1]
		// Update existing entry if name matches.
		for _, item := range seq.Content {
			for j := 0; j+1 < len(item.Content); j += 2 {
				if item.Content[j].Value == "name" && item.Content[j+1].Value == col.Name {
					for k := 0; k+1 < len(item.Content); k += 2 {
						if item.Content[k].Value == "path" {
							item.Content[k+1].Value = col.Path
							return writeConfigNode(configPath, &doc)
						}
					}
					// name found but no path key — append one
					item.Content = append(item.Content,
						&yaml.Node{Kind: yaml.ScalarNode, Value: "path"},
						&yaml.Node{Kind: yaml.ScalarNode, Value: col.Path},
					)
					return writeConfigNode(configPath, &doc)
				}
			}
		}
		// Not found — append new entry.
		seq.Content = append(seq.Content, collectionToNode(col))
		return writeConfigNode(configPath, &doc)
	}

	// No collections key at all — append one.
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	seq.Content = append(seq.Content, collectionToNode(col))
	root.Content = append(root.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: "collections"},
		seq,
	)
	return writeConfigNode(configPath, &doc)
}

// RenameCollection changes the name of an existing collection in the config file
// at configPath. It performs a single read-modify-write so there is no window
// where the old entry has been deleted but the new entry has not yet been written.
func RenameCollection(configPath, oldName, newName string) error {
	if configPath == "" {
		configPath = DefaultConfigPath()
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parsing config: %w", err)
	}
	if len(doc.Content) == 0 {
		return fmt.Errorf("empty config document")
	}
	root := doc.Content[0]

	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "collections" {
			continue
		}
		seq := root.Content[i+1]
		for _, item := range seq.Content {
			for j := 0; j+1 < len(item.Content); j += 2 {
				if item.Content[j].Value == "name" && item.Content[j+1].Value == oldName {
					item.Content[j+1].Value = newName
					return writeConfigNode(configPath, &doc)
				}
			}
		}
		return fmt.Errorf("collection %q not found in config", oldName)
	}
	return fmt.Errorf("collection %q not found in config", oldName)
}

// RemoveCollection removes a named collection from the config file at configPath.
// Existing YAML comments and structure are preserved via yaml.Node round-trip.
// Returns an error if the collection name is not found.
func RemoveCollection(configPath string, name string) error {
	if configPath == "" {
		configPath = DefaultConfigPath()
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parsing config: %w", err)
	}
	if len(doc.Content) == 0 {
		return fmt.Errorf("empty config document")
	}
	root := doc.Content[0]

	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "collections" {
			continue
		}
		seq := root.Content[i+1]
		for j, item := range seq.Content {
			for k := 0; k+1 < len(item.Content); k += 2 {
				if item.Content[k].Value == "name" && item.Content[k+1].Value == name {
					seq.Content = append(seq.Content[:j], seq.Content[j+1:]...)
					return writeConfigNode(configPath, &doc)
				}
			}
		}
		return fmt.Errorf("collection %q not found in config", name)
	}
	return fmt.Errorf("collection %q not found in config", name)
}

func collectionToNode(col Collection) *yaml.Node {
	m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	add := func(k, v string) {
		m.Content = append(m.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: k},
			&yaml.Node{Kind: yaml.ScalarNode, Value: v},
		)
	}
	add("name", col.Name)
	add("path", col.Path)
	if col.Description != "" {
		add("description", col.Description)
	}
	return m
}

func writeConfigNode(configPath string, doc *yaml.Node) error {
	out, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	return os.WriteFile(configPath, out, 0644)
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
