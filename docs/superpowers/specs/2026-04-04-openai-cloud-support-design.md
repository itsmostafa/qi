# OpenAI Cloud Support Design

**Date:** 2026-04-04
**Status:** Approved

## Summary

Add first-class OpenAI cloud support for both the generation and embedding providers. When a user sets `name: openai` in their provider config, qi automatically supplies the official OpenAI base URL and reads the API key from the `OPENAI_API_KEY` environment variable (unless overridden in config).

## Motivation

The providers already call OpenAI-compatible endpoints (`/v1/chat/completions`, `/v1/embeddings`). The only missing pieces are: (1) automatic base URL for the `openai` named provider, (2) `OPENAI_API_KEY` env var support, and (3) auth header support in the embedding provider (which currently always sends unauthenticated requests).

## Design

### Approach: Named preset `openai` in config

The user opts in explicitly by writing `name: openai` in their config. This is the clearest signal of intent and keeps env var semantics unambiguous — `OPENAI_API_KEY` only applies when the user has chosen the `openai` provider.

### Config changes (`internal/config/config.go`)

1. Add `APIKey string \`yaml:"api_key,omitempty"\`` to `EmbeddingProviderConfig` (generation already has this field).
2. Add `applyEnvOverrides()` called inside `Load()` after unmarshalling, before validation. For each provider (embedding, rerank, generation) where `name == "openai"`:
   - If `base_url` is empty, set it to `https://api.openai.com`.
   - If `api_key` is empty, read `os.Getenv("OPENAI_API_KEY")` and assign it.

### Config template changes (`internal/config/defaults.go`)

Add commented OpenAI examples in `DefaultConfigTemplate` alongside the existing Ollama examples:

```yaml
  # OpenAI cloud — set OPENAI_API_KEY in your environment
  # generation:
  #   name: openai
  #   model: gpt-4o-mini

  # OpenAI embeddings — set OPENAI_API_KEY in your environment
  # embedding:
  #   name: openai
  #   model: text-embedding-3-small
  #   dimension: 1536
```

### Provider changes (`internal/providers/embedding.go`)

Send `Authorization: Bearer <api_key>` when `cfg.APIKey` is non-empty. This mirrors the existing pattern in `generation.go:72-74`. No interface changes.

## Files Changed

| File | Change |
|------|--------|
| `internal/config/config.go` | `APIKey` on `EmbeddingProviderConfig`; `applyEnvOverrides()` |
| `internal/config/defaults.go` | OpenAI examples in config template |
| `internal/providers/embedding.go` | Auth header when `APIKey` set |

## Out of Scope

- Rerank provider OpenAI support (OpenAI has no rerank endpoint)
- Auto-wiring OpenAI when no provider is configured (too opinionated, requires picking a default model)
- Any new files, interfaces, or packages
