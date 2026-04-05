package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad_ValidConfig(t *testing.T) {
	path := writeTempConfig(t, `
database_path: /tmp/test.db
collections:
  - name: docs
    path: /tmp
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.DatabasePath != "/tmp/test.db" {
		t.Errorf("unexpected db path: %s", cfg.DatabasePath)
	}
	if len(cfg.Collections) != 1 || cfg.Collections[0].Name != "docs" {
		t.Errorf("unexpected collections: %+v", cfg.Collections)
	}
}

func TestLoad_Defaults(t *testing.T) {
	path := writeTempConfig(t, `
collections:
  - name: notes
    path: /tmp
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Search.BM25TopK != 50 {
		t.Errorf("expected default BM25TopK=50, got %d", cfg.Search.BM25TopK)
	}
	if cfg.Search.RRFK != 60 {
		t.Errorf("expected default RRFK=60, got %d", cfg.Search.RRFK)
	}
}

func TestLoad_DuplicateCollection(t *testing.T) {
	path := writeTempConfig(t, `
collections:
  - name: docs
    path: /tmp
  - name: docs
    path: /var
`)
	_, err := Load(path)
	if err == nil {
		t.Error("expected error for duplicate collection name")
	}
}

func TestLoad_MissingPath(t *testing.T) {
	path := writeTempConfig(t, `
collections:
  - name: docs
`)
	_, err := Load(path)
	if err == nil {
		t.Error("expected error for collection missing path")
	}
}

func TestLoad_RelativePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
collections:
  - name: docs
    path: ./subdir
`), 0o640); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	expected := filepath.Join(dir, "subdir")
	if cfg.Collections[0].Path != expected {
		t.Errorf("expected %q, got %q", expected, cfg.Collections[0].Path)
	}
}

func TestLoad_OpenAIGeneration_EnvKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test-gen")
	path := writeTempConfig(t, `
collections:
  - name: docs
    path: /tmp
providers:
  generation:
    name: openai
    model: gpt-4o-mini
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Providers.Generation == nil {
		t.Fatal("expected generation provider")
	}
	if cfg.Providers.Generation.BaseURL != "https://api.openai.com" {
		t.Errorf("expected base_url=https://api.openai.com, got %q", cfg.Providers.Generation.BaseURL)
	}
	if cfg.Providers.Generation.APIKey != "sk-test-gen" {
		t.Errorf("expected api_key=sk-test-gen, got %q", cfg.Providers.Generation.APIKey)
	}
}

func TestLoad_OpenAIEmbedding_EnvKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test-emb")
	path := writeTempConfig(t, `
collections:
  - name: docs
    path: /tmp
providers:
  embedding:
    name: openai
    model: text-embedding-3-small
    dimension: 1536
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Providers.Embedding == nil {
		t.Fatal("expected embedding provider")
	}
	if cfg.Providers.Embedding.BaseURL != "https://api.openai.com" {
		t.Errorf("expected base_url=https://api.openai.com, got %q", cfg.Providers.Embedding.BaseURL)
	}
	if cfg.Providers.Embedding.APIKey != "sk-test-emb" {
		t.Errorf("expected api_key=sk-test-emb, got %q", cfg.Providers.Embedding.APIKey)
	}
}

func TestLoad_OpenAI_ConfigKeyTakesPrecedence(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-from-env")
	path := writeTempConfig(t, `
collections:
  - name: docs
    path: /tmp
providers:
  generation:
    name: openai
    model: gpt-4o
    api_key: sk-from-config
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Providers.Generation.APIKey != "sk-from-config" {
		t.Errorf("config key should win over env, got %q", cfg.Providers.Generation.APIKey)
	}
}

func TestLoad_OpenAI_ExplicitBaseURLPreserved(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	path := writeTempConfig(t, `
collections:
  - name: docs
    path: /tmp
providers:
  generation:
    name: openai
    model: gpt-4o
    base_url: https://custom.proxy.example.com
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Providers.Generation.BaseURL != "https://custom.proxy.example.com" {
		t.Errorf("explicit base_url should be preserved, got %q", cfg.Providers.Generation.BaseURL)
	}
}

func TestLoad_NonOpenAI_NoEnvKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-should-not-apply")
	path := writeTempConfig(t, `
collections:
  - name: docs
    path: /tmp
providers:
  generation:
    name: ollama
    base_url: http://localhost:11434
    model: llama3.2
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Providers.Generation.APIKey != "" {
		t.Errorf("OPENAI_API_KEY should not apply to non-openai providers, got %q", cfg.Providers.Generation.APIKey)
	}
}

func TestExpandHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	tests := []struct {
		input    string
		expected string
	}{
		{"~/foo", filepath.Join(home, "foo")},
		{"/absolute", "/absolute"},
		{"relative", "relative"},
	}
	for _, tt := range tests {
		got := ExpandHome(tt.input)
		if got != tt.expected {
			t.Errorf("ExpandHome(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
