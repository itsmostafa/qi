package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Collection struct {
	Name         string   `yaml:"name"`
	Path         string   `yaml:"path"`
	Description  string   `yaml:"description,omitempty"`
	Extensions   []string `yaml:"extensions,omitempty"`
	Ignore       []string `yaml:"ignore,omitempty"`
	OriginalName string   `yaml:"-"`
}

type EmbeddingProviderConfig struct {
	Name          string `yaml:"name"`
	BaseURL       string `yaml:"base_url"`
	APIKey        string `yaml:"api_key,omitempty"`
	Model         string `yaml:"model"`
	Dimension     int    `yaml:"dimension"`
	BatchSize     int    `yaml:"batch_size,omitempty"`
	MaxInputChars int    `yaml:"max_input_chars,omitempty"`
}

// Fingerprint identifies the provider endpoint and embedding settings that
// produced a vector. API keys and batching are intentionally excluded.
func (c *EmbeddingProviderConfig) Fingerprint() string {
	if c == nil {
		return ""
	}
	endpoint := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if u, err := url.Parse(endpoint); err == nil {
		u.Scheme = strings.ToLower(u.Scheme)
		u.Host = strings.ToLower(u.Host)
		endpoint = strings.TrimRight(u.String(), "/")
	}
	raw := "provider=" + c.Name + "|endpoint=" + endpoint + "|model=" + c.Model +
		"|dim=" + strconv.Itoa(c.Dimension) + "|maxchars=" + strconv.Itoa(c.MaxInputChars)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

type RerankProviderConfig struct {
	Name    string `yaml:"name"`
	BaseURL string `yaml:"base_url"`
	Model   string `yaml:"model"`
}

type Providers struct {
	Embedding *EmbeddingProviderConfig `yaml:"embedding,omitempty"`
	Rerank    *RerankProviderConfig    `yaml:"rerank,omitempty"`
}

type SearchConfig struct {
	DefaultMode      string   `yaml:"default_mode"`
	BM25TopK         int      `yaml:"bm25_top_k"`
	VectorTopK       int      `yaml:"vector_top_k"`
	RerankTopK       int      `yaml:"rerank_top_k"`
	RRFK             int      `yaml:"rrf_k"`
	ChunkSize        int      `yaml:"chunk_size"`
	ChunkOverlap     int      `yaml:"chunk_overlap"`
	PreferExtensions []string `yaml:"prefer_extensions,omitempty"`
	ExtensionBoost   float64  `yaml:"extension_boost,omitempty"`
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
	cfg.expandEnvVars()
	cfg.resolveRelativePaths(configDir)
	cfg.normalizeCollectionNames()
	cfg.applyEnvOverrides()

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

// normalizeCollectionNames replaces configured names with generated ones,
// remembering the old name so indexed rows can be migrated. Names are assigned
// together because uniqueness is a property of the set, not of one path.
func (c *Config) normalizeCollectionNames() {
	paths := make([]string, len(c.Collections))
	for i := range c.Collections {
		paths[i] = c.Collections[i].Path
	}
	generated := AssignCollectionNames(paths)
	for i := range c.Collections {
		if c.Collections[i].Name != "" && c.Collections[i].Name != generated[i] {
			c.Collections[i].OriginalName = c.Collections[i].Name
		}
		c.Collections[i].Name = generated[i]
	}
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

// expandEnvVars expands ${VAR} references in provider api_key and base_url fields.
func (c *Config) expandEnvVars() {
	if emb := c.Providers.Embedding; emb != nil {
		emb.APIKey = os.ExpandEnv(emb.APIKey)
		emb.BaseURL = os.ExpandEnv(emb.BaseURL)
	}
	if rer := c.Providers.Rerank; rer != nil {
		rer.BaseURL = os.ExpandEnv(rer.BaseURL)
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

// AddCollection adds or updates a collection in the config file at configPath.
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
	configDir := configFileDir(configPath)

	// Find the collections sequence node in the root mapping.
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "collections" {
			continue
		}
		seq := root.Content[i+1]

		// Name the whole set at once. Uniqueness is a property of the set, so
		// naming this collection alone would hand a colliding basename to the
		// check below and reject an addition the loader would have accepted.
		resolved := make([]string, len(seq.Content))
		for j, item := range seq.Content {
			resolved[j] = resolveConfigFilePath(configDir, mappingValue(item, "path"))
		}
		assigned := AssignCollectionNames(append(append([]string{}, resolved...), col.Path))
		col.Name = assigned[len(assigned)-1]

		// Update an existing entry only when its canonical path matches. This
		// lets old configs with custom names be normalized without allowing a
		// generated-name collision to rewrite another collection's path.
		for idx, item := range seq.Content {
			itemName := mappingValue(item, "name")
			itemPath := mappingValue(item, "path")
			resolvedItemPath := resolved[idx]
			generatedName := ""
			if itemPath != "" {
				generatedName = assigned[idx]
			}
			if !sameCanonicalPath(resolvedItemPath, col.Path) {
				if itemName == col.Name || generatedName == col.Name {
					return fmt.Errorf("collection name %q collides with existing path %q", col.Name, itemPath)
				}
				continue
			}
			for j := 0; j+1 < len(item.Content); j += 2 {
				switch item.Content[j].Value {
				case "name":
					item.Content[j+1].Value = col.Name
				case "path":
					item.Content[j+1].Value = col.Path
				}
			}
			if mappingValue(item, "name") == "" {
				item.Content = append(item.Content,
					&yaml.Node{Kind: yaml.ScalarNode, Value: "name"},
					&yaml.Node{Kind: yaml.ScalarNode, Value: col.Name},
				)
			}
			if mappingValue(item, "path") == "" {
				item.Content = append(item.Content,
					&yaml.Node{Kind: yaml.ScalarNode, Value: "path"},
					&yaml.Node{Kind: yaml.ScalarNode, Value: col.Path},
				)
			}
			return writeConfigNode(configPath, &doc)
		}
		// Not found — append new entry.
		seq.Content = append(seq.Content, collectionToNode(col))
		return writeConfigNode(configPath, &doc)
	}

	// No collections key at all — this is the only collection, so its own
	// directory name is unambiguous.
	col.Name = SlugFromPath(col.Path)
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	seq.Content = append(seq.Content, collectionToNode(col))
	root.Content = append(root.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: "collections"},
		seq,
	)
	return writeConfigNode(configPath, &doc)
}

// RemoveCollection removes a collection from the config file at configPath.
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
	configDir := configFileDir(configPath)

	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "collections" {
			continue
		}
		seq := root.Content[i+1]

		// Name the whole set, the way Load does. A per-path basename would miss
		// the lengthened name a collision produced ("work-notes"), and deleting
		// data under a name the config cannot then find loses the entry.
		resolved := make([]string, len(seq.Content))
		for j, item := range seq.Content {
			resolved[j] = resolveConfigFilePath(configDir, mappingValue(item, "path"))
		}
		assigned := AssignCollectionNames(resolved)

		exactIndex := -1
		exactMatches := 0
		legacyIndex := -1
		legacyMatches := 0
		for j, item := range seq.Content {
			itemName := mappingValue(item, "name")
			itemPath := mappingValue(item, "path")
			generatedName := ""
			if itemPath != "" {
				generatedName = assigned[j]
			}
			if generatedName == name {
				exactIndex = j
				exactMatches++
			}
			if itemName == name {
				legacyIndex = j
				legacyMatches++
			}
		}
		if exactMatches > 1 {
			return fmt.Errorf("collection %q is ambiguous in config", name)
		}
		if exactMatches == 1 {
			seq.Content = append(seq.Content[:exactIndex], seq.Content[exactIndex+1:]...)
			return writeConfigNode(configPath, &doc)
		}
		if legacyMatches > 1 {
			return fmt.Errorf("collection %q is ambiguous in config", name)
		}
		if legacyMatches == 1 {
			seq.Content = append(seq.Content[:legacyIndex], seq.Content[legacyIndex+1:]...)
			return writeConfigNode(configPath, &doc)
		}
		return fmt.Errorf("collection %q not found in config", name)
	}
	return fmt.Errorf("collection %q not found in config", name)
}

func mappingValue(node *yaml.Node, key string) string {
	if node == nil {
		return ""
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1].Value
		}
	}
	return ""
}

func collectionToNode(col Collection) *yaml.Node {
	m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	addStr := func(k, v string) {
		m.Content = append(m.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: k},
			&yaml.Node{Kind: yaml.ScalarNode, Value: v},
		)
	}
	addSeq := func(k string, vals []string) {
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, v := range vals {
			seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: v})
		}
		m.Content = append(m.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: k},
			seq,
		)
	}
	addStr("name", col.Name)
	addStr("path", col.Path)
	if col.Description != "" {
		addStr("description", col.Description)
	}
	if len(col.Extensions) > 0 {
		addSeq("extensions", col.Extensions)
	}
	if len(col.Ignore) > 0 {
		addSeq("ignore", col.Ignore)
	}
	return m
}

func writeConfigNode(configPath string, doc *yaml.Node) error {
	out, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	return os.WriteFile(configPath, out, 0o600)
}

func (c *Config) validate() error {
	if c.Providers.Embedding != nil && c.Providers.Embedding.Dimension <= 0 {
		return fmt.Errorf("embedding dimension must be positive, got %d", c.Providers.Embedding.Dimension)
	}
	seen := map[string]bool{}
	seenPaths := map[string]string{}
	for _, col := range c.Collections {
		if col.Path == "" {
			return fmt.Errorf("collection %q missing path", col.Name)
		}
		if col.Name == "" {
			return fmt.Errorf("collection %q has empty generated name", col.Path)
		}
		if seen[col.Name] {
			return fmt.Errorf("duplicate collection name %q", col.Name)
		}
		seen[col.Name] = true
		canonical := canonicalPath(col.Path)
		if existing, ok := seenPaths[canonical]; ok {
			return fmt.Errorf("duplicate collection path %q used by %q and %q", col.Path, existing, col.Name)
		}
		seenPaths[canonical] = col.Name
	}
	return nil
}

func sameCanonicalPath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return canonicalPath(ExpandHome(a)) == canonicalPath(ExpandHome(b))
}

func configFileDir(configPath string) string {
	if abs, err := filepath.Abs(configPath); err == nil {
		return filepath.Dir(abs)
	}
	return filepath.Dir(configPath)
}

func resolveConfigFilePath(baseDir, path string) string {
	path = ExpandHome(path)
	if path != "" && !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	return path
}

func canonicalPath(path string) string {
	canonical, err := CanonicalPath(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return canonical
}

// CanonicalPath expands a filesystem path to an absolute, symlink-resolved,
// clean path. Symlink resolution is best effort so nonexistent paths can still
// be compared by their cleaned absolute form.
func CanonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(ExpandHome(path))
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	return filepath.Clean(abs), nil
}
