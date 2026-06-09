# Built-in Embedding via Managed llama.cpp Sidecar — Design

**Date:** 2026-06-09
**Status:** Approved pending review

## Motivation

Today, vector search requires users to install and run an external embedding
provider (Ollama, LM Studio, llama.cpp), pick a model, and keep its
configuration consistent with the index. This produces the failure modes found
in the 2026-06 audit: missing task prefixes silently degrade retrieval,
switching models silently corrupts the vector space, dimension mismatches are
never validated, and `qi doctor` cannot diagnose any of it.

qi will instead ship a **built-in embedding path**: a pinned embedding model
and a pinned llama.cpp server binary, both auto-downloaded on first use
(with consent), run as a managed child process. qi owns the model choice,
its task prompts, and its dimension. Users configure nothing.

**Generation stays external** (Ollama / LM Studio / OpenAI-compatible).
Embedding is where silent index corruption lives and where models are small;
generation failures are loud and Ollama already handles big models well.

## Scope and platform

- Apple Silicon (M1 and newer) **only** for the built-in path.
  On any other `GOOS/GOARCH`, built-in embedding reports itself unavailable
  and qi degrades to BM25-only. An explicitly configured external embedding
  provider continues to work on any platform.
- qi itself remains a single CGo-free Go binary. No inference is linked in.

## Pinned artifacts

| Artifact | Source | Pinning |
|---|---|---|
| `Qwen3-Embedding-0.6B-Q8_0.gguf` (~640 MB, 1024-dim) | Hugging Face repo `Qwen/Qwen3-Embedding-0.6B-GGUF` (Apache-2.0, ungated, first-party) | HF revision (commit hash) + SHA-256, hardcoded in qi |
| `llama-server` (darwin-arm64, Metal) | `ggml-org/llama.cpp` GitHub release asset (`llama-<tag>-bin-macos-arm64.zip`) | release tag + SHA-256, hardcoded in qi |

Exact revision/tag/checksum values are resolved once at implementation time
and recorded in a single `internal/modelhub/pins.go` file. No "latest"
resolution ever happens at runtime. Bumping either pin is a code change that
triggers the stale-vector re-embed flow (below).

Model choice rationale: Qwen3-Embedding-0.6B is current-generation retrieval
quality (at or above embeddinggemma-300m), distributed ungated and first-party
under Apache-2.0. Rejected: `embeddinggemma-300m` (official HF repo is gated
behind a license click-through; ungated copies are third-party mirrors),
`nomic-embed-text-v1.5` (clean license but a 2024-generation model,
measurably weaker on paraphrase-style queries).

## Architecture

```
qi <command needing embeddings>
  └─ app.New()
      ├─ modelhub.Ensure(ctx)        download-if-missing, SHA-256 verified
      │     $XDG_DATA_HOME/qi/models/qwen3-embedding-0.6b/<file>.gguf
      │     $XDG_DATA_HOME/qi/bin/llama-server-<tag>
      ├─ sidecar.Start(ctx)          spawn llama-server, wait for /health
      └─ providers.NewEmbedding()    existing HTTP client, base_url = http://127.0.0.1:<port>
```

The existing OpenAI-compatible embedding provider
(`internal/providers/embedding.go`) is reused unchanged. New packages:

### `internal/modelhub`

- `Ensure(ctx) (Artifacts, error)` — returns paths to model + server binary,
  downloading whatever is missing.
- Downloads go to a temp file in the destination directory, SHA-256 is
  verified, then an atomic rename installs the artifact. A failed or
  truncated download leaves nothing behind. Progress is written to stderr.
- Respects `XDG_DATA_HOME` exactly like `config.DefaultDBPath()`.

### `internal/sidecar`

- `Start(ctx, Artifacts) (*Server, error)`:
  1. Read pidfile (`<data-dir>/sidecar.pid`, JSON: pid, port, model id).
     If it names a live process, kill it (crash hygiene: a SIGKILLed qi
     leaves an orphan only until the next run, and never two servers).
  2. Claim a free port by binding `127.0.0.1:0` and closing.
  3. Spawn `llama-server -m <gguf> --embeddings --pooling last
     --host 127.0.0.1 --port <p>`.
  4. Poll `GET /health` until ready (timeout 60s; model load is ~1–2s on
     Apple Silicon).
  5. Write pidfile.
- `Close()` — SIGTERM the child, remove pidfile. Called from `app.Close()`.
- The sidecar is started **lazily**: only by operations that actually embed
  (indexing, hybrid query, ask). BM25-only commands never pay the startup.
- v1 deliberately kills the server at the end of every invocation.
  Cross-invocation reuse (keep-alive) is future work.

### Prefix ownership

Qwen3-Embedding conventions are hardcoded for the built-in profile:

- Documents (index time): embedded raw.
- Queries (search time): embedded as
  `Instruct: Given a web search query, retrieve relevant passages that answer the query\nQuery: {q}`

Implemented as a thin prefixing wrapper around `EmbeddingProvider` with
distinct document/query entry points; `indexer.Embedder` uses the document
path, `search.Hybrid` uses the query path. External providers get empty
prefixes by default (configurable `document_prefix` / `query_prefix` keys are
added for them, closing the audit gap for nomic/embeddinggemma users).

## Configuration surface

```yaml
providers:
  # absent            -> built-in embedding (new default)
  # embedding:
  #   name: none      -> vectors disabled, BM25 only (written when consent declined)
  #   name: builtin   -> explicit built-in; optional server_binary: /path/override
  #   name: ollama|openai|...  -> existing external providers, unchanged
```

- Absent config now means **built-in**, where it previously meant "no
  vectors". The consent flow (below) keeps this from being a surprise.
- Generation config is untouched.

## Consent and first run

The first command that needs embeddings prompts on a TTY:

> qi needs to download the embedding model (~640 MB, one time) to enable
> semantic search. Proceed? [Y/n]

- Yes → download, proceed.
- No → write `providers.embedding: {name: none}` to the config so the user
  is asked exactly once; continue BM25-only.
- No TTY (agents, CI, pipes) → do not prompt, do not download; warn once and
  continue BM25-only. `qi init` offers the same prompt interactively so users
  can pre-download.

## Index integrity and migration

The existing `embeddings` table (chunk_id, provider, model, dimension) becomes
load-bearing:

- **Stale vector** := stored model/dimension differs from the active profile
  (e.g. a pre-existing nomic-via-Ollama index, or a future pin bump).
- `qi index` / `qi update` re-embed stale chunks. When the stale set is the
  entire collection, show the count and confirm before re-embedding.
- The query path never mixes vector spaces: if stale vectors exist for the
  queried collection, hybrid search warns and uses BM25 only until re-embedding
  completes.
- The embedder validates that every returned vector has the expected dimension
  (1024 for the built-in profile) before storing; mismatch is a hard error.

This closes audit findings: missing prefixes, silent model-switch corruption,
and unvalidated dimensions.

## Failure modes

| Failure | Behavior |
|---|---|
| Download checksum mismatch / truncation | Delete temp file, clear error, nothing installed |
| Offline, artifacts cached | Fully functional |
| Offline, artifacts missing | Warn once, BM25 fallback |
| Sidecar fails to start or dies mid-run | Warn, BM25 fallback (same contract as existing `hybrid.Search` degradation) |
| qi killed hard (orphan sidecar) | Next run's pidfile check kills it |
| Non-Apple-Silicon platform | Built-in unavailable message; external provider config still honored |

The server binary default is always the managed pinned download; a brew- or
PATH-installed llama.cpp is never picked up implicitly (uncontrolled version
= the variable this design exists to eliminate). `server_binary` config
overrides for those who insist.

## Doctor

`qi doctor` gains real provider checks:

- artifacts present and checksums valid
- sidecar starts and returns a 1024-dim embedding for a test string
- stale / unembedded chunk counts per collection
- generation provider reachability (HTTP probe)

## Testing

Follows existing repo patterns (real in-memory SQLite, `httptest` servers):

- `modelhub`: fake HF/GitHub via `httptest` — happy path, checksum mismatch,
  truncated download, already-installed skip, atomic install.
- `sidecar`: a stub `llama-server` test binary honoring `--port`, `/health`,
  and `/v1/embeddings` — startup, health timeout, stale-pidfile cleanup,
  Close semantics.
- `indexer/search`: stale-model re-embed selection, dimension validation,
  prefix application (document vs query), BM25 fallback when stale vectors
  exist.
- One opt-in end-to-end test against real llama.cpp + real model, gated by
  an env var (e.g. `QI_TEST_REAL_LLAMA=1`), skipped in CI.

## Out of scope (this design)

- Generation provider changes (stays external).
- The `ask` full-chunk-context fix and other audit items not listed above —
  separate work.
- Sidecar keep-alive / cross-invocation reuse — future optimization.
- Matryoshka dimension reduction (Qwen3 supports it; we pin 1024).
- Intel macOS, Linux, Windows built-in support.
