# qi — Claude Code Guidance

## Project Overview

qi is a local-first knowledge search CLI for macOS. It indexes documents (Markdown, plaintext, source code) into a SQLite database and provides BM25 full-text search, vector search (with local embedding providers), and LLM-powered Q&A with citations.

## Build

```sh
go build .          # Build binary
go test ./...       # Run all tests
go vet ./...        # Lint
```

## Checks

Always run `task check` before finishing any code change to ensure all checks pass (build, tests, vet).

## Key Design Decisions

- **CGo-free SQLite**: `github.com/ncruces/go-sqlite3` (wasm2go transpiled, no CGo needed)
- **Vector search**: Pure Go KNN with cosine distance stored as BLOBs. sqlite-vec was planned but has WASM compatibility issues with the current go-sqlite3 version — revisit when sqlite-vec-go-bindings updates to support newer go-sqlite3.
- **Content-addressable storage**: `content` table keyed by SHA-256 hash; `documents` references by hash. Enables deduplication and O(1) change detection.
- **Break-point chunker**: Scores chunk boundaries by type (heading=100, code fence=80, blank line=20) with distance decay from target size.
- **Graceful degradation**: Vector search and generation are optional — BM25 always works.
- **Config**: Raw `gopkg.in/yaml.v3`, no viper. `~` expansion + relative path resolution.

## Package Structure

```
cmd/                  Cobra commands (root, init, index, search, query, ask, get, doctor, stats)
internal/
  app/                Wires config + db + services
  config/             Config loading, defaults, path expansion
  db/                 SQLite open/migrate/WAL, embedding blob storage
    migrations/       Embedded SQL migrations (001_init.sql)
  chunker/            Break-point chunker (chunker.Chunker interface)
  indexer/            Filesystem walker, SHA-256 change detection, embedder
  output/             Text/JSON/Markdown formatters
  parser/             Document parsers (Markdown via goldmark, plaintext, source)
  providers/          HTTP adapters for embedding, rerank, generation APIs
  search/             BM25, vector KNN, RRF fusion, hybrid, ask, cache, prompt
  version/            Build-time version injection
```

## Testing

Tests use real in-memory SQLite (no mocking). Provider tests use `httptest.NewServer`.

## Adding a New Parser

1. Create `internal/parser/myformat.go`
2. Implement `Parser` interface
3. Call `Register(".ext", &myParser{})` in `init()`

## Adding a New Migration

Add `internal/db/migrations/00N_description.sql` — the runner applies them in alphabetical order.

## sqlite-vec Note

The plan called for `sqlite-vec` via `github.com/asg017/sqlite-vec-go-bindings/ncruces`. The bindings require `go-sqlite3 ≤ v0.17.1` (which uses wazero as WASM runtime), but the sqlite-vec WASM binary requires atomic instructions that wazero v1.7.3 doesn't enable by default. `go-sqlite3 ≥ v0.18` uses `wasm2go` (no wazero) and removed `sqlite3.Binary`. Until a compatible version of sqlite-vec-go-bindings is released, vector search uses pure Go KNN.
