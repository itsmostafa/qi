# OpenAI Cloud Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow users to use OpenAI's cloud API (generation + embeddings) by setting `name: openai` in config and exporting `OPENAI_API_KEY`.

**Architecture:** A post-unmarshal `applyEnvOverrides()` method in `config.Load()` auto-fills `base_url` and `api_key` for any provider named `openai`. The embedding provider gains an `APIKey` config field and sends the auth header when set. No new files, no new interfaces.

**Tech Stack:** Go standard library (`os`, `net/http`), `gopkg.in/yaml.v3`, `net/http/httptest` for tests.

---

### Task 1: Add `APIKey` to `EmbeddingProviderConfig` and write failing env-override tests

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Add `APIKey` field to `EmbeddingProviderConfig` in `config.go`**

In `internal/config/config.go`, change `EmbeddingProviderConfig` from:

```go
type EmbeddingProviderConfig struct {
	Name      string `yaml:"name"`
	BaseURL   string `yaml:"base_url"`
	Model     string `yaml:"model"`
	Dimension int    `yaml:"dimension"`
	BatchSize int    `yaml:"batch_size,omitempty"`
}
```

to:

```go
type EmbeddingProviderConfig struct {
	Name      string `yaml:"name"`
	BaseURL   string `yaml:"base_url"`
	APIKey    string `yaml:"api_key,omitempty"`
	Model     string `yaml:"model"`
	Dimension int    `yaml:"dimension"`
	BatchSize int    `yaml:"batch_size,omitempty"`
}
```

- [ ] **Step 2: Write failing tests for `applyEnvOverrides` in `config_test.go`**

Add these tests at the bottom of `internal/config/config_test.go`:

```go
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
```

- [ ] **Step 3: Run tests to confirm they fail**

```bash
go test ./internal/config/... -run "TestLoad_OpenAI" -v
```

Expected: FAIL — `applyEnvOverrides` does not exist yet, base_url and api_key are not populated.

---

### Task 2: Implement `applyEnvOverrides()` in `config.go`

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Add `applyEnvOverrides` method and call it from `Load`**

Add the following method to `internal/config/config.go` (add `"os"` to imports if not already present — it already is):

```go
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
```

Then in `Load()`, call `cfg.applyEnvOverrides()` after `cfg.expandPaths()` and before `cfg.validate()`:

```go
cfg.expandPaths()
cfg.resolveRelativePaths(configDir)
cfg.applyEnvOverrides()

if err := cfg.validate(); err != nil {
```

- [ ] **Step 2: Run the tests to confirm they pass**

```bash
go test ./internal/config/... -run "TestLoad_OpenAI" -v
```

Expected: all 5 tests PASS.

- [ ] **Step 3: Run `task check` to confirm all lint and tests pass**

```bash
task check
```

Expected: `go vet ./...` and `go test ./...` both pass with no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add APIKey to EmbeddingProviderConfig and applyEnvOverrides for openai preset"
```

---

### Task 3: Send auth header in embedding provider when `APIKey` is set

**Files:**
- Modify: `internal/providers/embedding.go`
- Modify: `internal/providers/embedding_test.go`

- [ ] **Step 1: Write a failing test that asserts the auth header is sent**

Add this test to `internal/providers/embedding_test.go`:

```go
func TestEmbeddingProvider_SendsAuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		resp := embeddingResponse{}
		resp.Data = append(resp.Data, struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		}{Embedding: []float32{1.0, 0.0}, Index: 0})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	cfg := &config.EmbeddingProviderConfig{
		BaseURL:   srv.URL,
		Model:     "test-model",
		Dimension: 2,
		APIKey:    "sk-test-key",
	}
	p := NewEmbedding(cfg)
	_, err := p.Embed(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if gotAuth != "Bearer sk-test-key" {
		t.Errorf("expected Authorization: Bearer sk-test-key, got %q", gotAuth)
	}
}

func TestEmbeddingProvider_NoAuthHeader_WhenKeyEmpty(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		resp := embeddingResponse{}
		resp.Data = append(resp.Data, struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		}{Embedding: []float32{1.0, 0.0}, Index: 0})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	cfg := &config.EmbeddingProviderConfig{
		BaseURL:   srv.URL,
		Model:     "test-model",
		Dimension: 2,
	}
	p := NewEmbedding(cfg)
	_, err := p.Embed(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("expected no Authorization header, got %q", gotAuth)
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/providers/... -run "TestEmbeddingProvider_SendsAuthHeader|TestEmbeddingProvider_NoAuthHeader" -v
```

Expected: `TestEmbeddingProvider_SendsAuthHeader` FAIL — auth header is never set.

- [ ] **Step 3: Add auth header in `embedBatch` in `embedding.go`**

In `internal/providers/embedding.go`, in the `embedBatch` method, add the auth header after `req.Header.Set("Content-Type", "application/json")`:

```go
req.Header.Set("Content-Type", "application/json")
if p.cfg.APIKey != "" {
    req.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
}
```

The updated block in context:

```go
req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
if err != nil {
    return nil, err
}
req.Header.Set("Content-Type", "application/json")
if p.cfg.APIKey != "" {
    req.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
}

resp, err := p.client.Do(req)
```

- [ ] **Step 4: Run `task check` to confirm all lint and tests pass**

```bash
task check
```

Expected: `go vet ./...` and `go test ./...` both pass with no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/providers/embedding.go internal/providers/embedding_test.go
git commit -m "feat(providers): send Authorization header in embedding provider when api_key is set"
```

---

### Task 4: Update config template and README

**Files:**
- Modify: `internal/config/defaults.go`
- Modify: `README.md`

- [ ] **Step 1: Add OpenAI examples to `DefaultConfigTemplate` in `defaults.go`**

In `internal/config/defaults.go`, update the providers block in `DefaultConfigTemplate` to include OpenAI examples. Replace:

```go
providers:
  # Uncomment to enable embeddings (llama.cpp / Ollama compatible)
  # embedding:
  #   name: ollama
  #   base_url: http://localhost:11434
  #   model: nomic-embed-text
  #   dimension: 768
  #   batch_size: 32

  # Uncomment to enable LLM generation
  # generation:
  #   name: ollama
  #   base_url: http://localhost:11434
  #   model: llama3.2
```

with:

```go
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
```

- [ ] **Step 2: Update README.md — description and features**

In `README.md`, update the one-liner description from:

```
A local-first knowledge search CLI for macOS and Linux. Index your documents and search them using BM25 full-text search, vector embeddings, and LLM-powered Q&A — all running locally with no external dependencies.
```

to:

```
A local-first knowledge search CLI for macOS and Linux. Index your documents and search them using BM25 full-text search, vector embeddings, and LLM-powered Q&A — running locally via Ollama/llama.cpp or using OpenAI's cloud models.
```

Then update the vector search feature bullet from:

```
- **Vector search that stays local** — embeddings stored and queried entirely on your machine; works with Ollama, LM Studio, llama.cpp, or any OpenAI-compatible provider
```

to:

```
- **Flexible vector search** — embeddings stored and queried on your machine; works with Ollama, LM Studio, llama.cpp, or OpenAI's cloud (`text-embedding-3-small`, etc.)
```

- [ ] **Step 3: Update README.md — Configuration section**

In `README.md`, in the `## Configuration` section, replace the providers block in the YAML example from:

```yaml
providers:
  embedding:
    base_url: http://localhost:11434  # Ollama
    model: nomic-embed-text
    dimension: 768

  generation:
    base_url: http://localhost:11434
    model: llama3.2
```

with:

```yaml
providers:
  # Local (Ollama / llama.cpp)
  embedding:
    name: ollama
    base_url: http://localhost:11434
    model: nomic-embed-text
    dimension: 768

  generation:
    name: ollama
    base_url: http://localhost:11434
    model: llama3.2

  # Or: OpenAI cloud (set OPENAI_API_KEY in your environment)
  # embedding:
  #   name: openai
  #   model: text-embedding-3-small
  #   dimension: 1536
  #
  # generation:
  #   name: openai
  #   model: gpt-4o-mini
```

- [ ] **Step 4: Run `task check` to confirm all lint and tests pass**

```bash
task check
```

Expected: `go vet ./...` and `go test ./...` both pass with no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/config/defaults.go README.md
git commit -m "docs: add OpenAI cloud provider examples to config template and README"
```
