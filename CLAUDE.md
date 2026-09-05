# qi — Agent Guidance

This file is the canonical guidance for AI coding agents working in this repo. `AGENTS.md` is a symlink to it, so Claude Code, Codex, and other agents read the same instructions.

## Project Overview

qi is a local-first knowledge search CLI for macOS. It indexes documents (Markdown, plaintext) into a SQLite database and provides BM25 full-text search and vector search (with local embedding providers).

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
- **Graceful degradation**: Vector search is optional — BM25 always works.
- **Auto-generated collection names**: A collection is named after its own directory (`~/Projects/tools/qi` → `qi`). Colliding names absorb leading path segments until unique (`work-notes`, `personal-notes`). The `--name` flag was removed from `index`. Legacy names are normalized on startup and indexed rows migrate via `RenameCollectionData`.
- **Every document has a date**: `doc_timestamp` falls back to the file's mtime when frontmatter carries no readable date (`internal/indexer.documentDate`). Without it, undated documents were NULL, and NULL satisfies neither `--since` nor `--until`, so the recency filters returned nothing on corpora that don't use dated frontmatter. A plain `qi index` backfills NULL rows without re-chunking or re-embedding.
- **Query relaxation**: BM25 search automatically falls back from conjunctive to disjunctive matching for natural-language queries that return zero results.
- **Frontmatter is document metadata, not body text**: `internal/parser/frontmatter.go` strips YAML frontmatter before goldmark parses. `title`, `timestamp`/`date`/`created` and `tags` become document-level fields; the useful ones are re-emitted as one leading section of plain prose so they stay searchable.
- **One result per document**: both retrievers keep only a document's best-ranked chunk — BM25 stops at `poolSize` *distinct documents* rather than applying a SQL `LIMIT` to chunks, and the vector KNN dedupes before truncating — and `ReciprocalRankFusion` keys on document ID, counting each document once at its best rank. Bounding the pool by chunks let one verbose file starve every other match.
- **Post-retrieval pass**: `search.Finalize` (`internal/search/postprocess.go`) applies date sort, duplicate-snippet collapse and a backstop one-per-document pass over the whole candidate pool, then truncates to the caller's limit. Retrievers no longer truncate; commands call `Finalize`.
- **Compaction on index**: every `qi index` hard-deletes deactivated documents, prunes orphaned content blobs, runs FTS5 `optimize`, and `VACUUM`s when the freelist exceeds a quarter of the file. A restored file therefore re-chunks and re-embeds rather than reactivating.
- **Resource limits**: files over 10 MiB are refused per-file (`maxFileSize`, `internal/indexer`); a read failure never deactivates a good document, but the run names the paths whose stale content is still searchable and exits nonzero.
- **Auto-embed on index**: When an embedder is configured, chunks are embedded immediately after indexing without a separate step.
- **Config**: Raw `gopkg.in/yaml.v3`, no viper. `~` expansion + relative path resolution.

## Package Structure

```
cmd/                  Cobra commands (root, init, index, search, query, get, doctor, stats, list, delete, update)
internal/
  app/                Wires config + db + services
  config/             Config loading, defaults, path expansion
  db/                 SQLite open/migrate/WAL, embedding blob storage
    migrations/       Embedded SQL migrations (001_init.sql … 006_document_metadata.sql)
  chunker/            Break-point chunker (chunker.Chunker interface)
  indexer/            Filesystem walker, SHA-256 change detection, embedder
  output/             Text/JSON formatters
  parser/             Document parsers (Markdown via goldmark, plaintext)
  providers/          HTTP adapters for embedding APIs
  search/             BM25, vector KNN, RRF fusion, hybrid
  version/            Build-time version injection
```

## Testing

Tests use real in-memory SQLite (no mocking). Provider tests use `httptest.NewServer`.

## Adding a New Parser

1. Create `internal/parser/myformat.go`
2. Implement `Parser` interface
3. Call `Register(".ext", &myParser{})` in `init()`

## Adding a New Migration

Add `internal/db/migrations/00N_description.sql` — the runner applies them in alphabetical order and skips versions already recorded in `schema_version`. Current migrations: `001_init.sql` … `006_document_metadata.sql`. An `ALTER TABLE ... ADD COLUMN` migration must also register one added column in `addedColumns` (`migrate.go`), since SQLite has no `IF NOT EXISTS` for it.

## sqlite-vec Note

The plan called for `sqlite-vec` via `github.com/asg017/sqlite-vec-go-bindings/ncruces`. The bindings require `go-sqlite3 ≤ v0.17.1` (which uses wazero as WASM runtime), but the sqlite-vec WASM binary requires atomic instructions that wazero v1.7.3 doesn't enable by default. `go-sqlite3 ≥ v0.18` uses `wasm2go` (no wazero) and removed `sqlite3.Binary`. Until a compatible version of sqlite-vec-go-bindings is released, vector search uses pure Go KNN.
