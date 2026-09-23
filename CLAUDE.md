# qi — Agent Guidance

This file is the canonical guidance for AI coding agents working in this repo. `AGENTS.md` is a symlink to it, so Claude Code, Codex, and other agents read the same instructions.

## Project Overview

qi is a local-first knowledge search CLI for macOS and Linux. It indexes documents (Markdown, MDX, reStructuredText, AsciiDoc, plaintext) into a SQLite database and provides BM25 full-text search and vector search (with local embedding providers).

## Working Style

- Treat requests for action as instructions to complete the work. Do not stop at acknowledging the request, proposing a plan, or offering to continue.
- Infer routine details from the request, repository context, and existing conventions. Make reasonable assumptions and persist until the intended outcome is complete.
- Before asking a clarifying question, finish the work already authorized by context and make any remaining decision concrete and reviewable. Ask only when the answer could materially change the result.
- Explicit user instructions take precedence over guidance in a skill. If a skill causes work to pause, remain unfinished, or diverge from the request, identify the exact skill instruction and explain how it applies.
- Do not add unsolicited warnings, approval steps, or checklists for hypothetical risks.

## Communication

- State the outcome or main point early. Use concise paragraphs with one main idea each.
- Prefer plain language and active voice. Include technical detail only when it helps the reader understand or verify the work.
- Use lists for genuinely parallel or sequential information and avoid unnecessary nesting, tables, headings, and repeated summaries.
- Avoid canned phrases, invented labels, and contrastive framing that introduces alternatives the user did not ask about.

## Delegation

- When the runtime and user instructions allow subagents, delegate independent work that can run in parallel and would materially save time or improve quality.
- Keep small or tightly coupled changes with one agent. Give delegated tasks clear boundaries, review their results, and integrate them into one coherent answer.
- Write inter-agent messages clearly because a person may read them.

## Build

```sh
go build .          # Build binary
go test ./...       # Run all tests
go vet ./...        # Lint
```

## Checks

- Always run `task check` before finishing a code change. Use focused tests while iterating, then run the required check once the implementation is ready.
- Do not add tests for a reversible, low-impact change when they would only mirror the implementation. Tests should verify meaningful behavior or guard against a plausible regression.
- After the required checks pass, broaden or repeat them only when another change, a failure, or an unresolved concern justifies it.
- Documentation-only edits do not require the full Go test suite unless they change executable examples, generated artifacts, or documented command behavior.

## Key Design Decisions

- **CGo-free SQLite**: `github.com/ncruces/go-sqlite3` (wasm2go transpiled, no CGo needed)
- **Vector search**: Pure Go KNN with cosine distance stored as BLOBs. sqlite-vec was planned but has WASM compatibility issues with the current go-sqlite3 version — revisit when sqlite-vec-go-bindings updates to support newer go-sqlite3.
- **Content-addressable storage**: `content` table keyed by SHA-256 hash; `documents` references by hash. Enables deduplication and O(1) change detection.
- **Break-point chunker**: Scores chunk boundaries by type (heading=100, code fence=80, blank line=20) with distance decay from target size.
- **Graceful degradation**: Vector search is optional — BM25 always works.
- **Auto-generated collection names**: A collection is named after its own directory (`~/Projects/tools/qi` → `qi`). Colliding names absorb leading path segments until unique (`work-notes`, `personal-notes`). The `--name` flag was removed from `index`. Legacy names are normalized on startup and indexed rows migrate via `RenameCollectionData`.
- **Every document has a date**: `doc_timestamp` falls back to the file's mtime when frontmatter carries no readable date (`internal/indexer.documentDate`). Without it, undated documents were NULL, and NULL satisfies neither `--since` nor `--until`, so the recency filters returned nothing on corpora that don't use dated frontmatter. A plain `qi index` backfills NULL rows and re-derives a fallback date whose mtime has moved (touch, cloud sync, `git checkout`), both without re-chunking or re-embedding; an explicit frontmatter date never follows the mtime.
- **One glob dialect for scope**: `config.PathMatch` backs both a collection's `ignore` list and the search `--path` filter — `path.Match` semantics applied to the collection-relative path, its basename and every ancestor, so `drafts` and `drafts/*` both cover `drafts/a/b.md`. `ignore` now applies to files as well as directories (it only ever checked directories, despite the docs). No negation and no `.gitignore` parsing: both need a dependency or a rule engine, and collections point at notes, not working trees. `--path` is applied inside the retrieval loops, before the per-document collapse and the limit, so an in-scope hit does not fall out behind higher-ranked out-of-scope ones.
- **Query relaxation**: BM25 search automatically falls back from conjunctive to disjunctive matching for natural-language queries that return zero results.
- **Frontmatter is document metadata, not body text**: `internal/parser/frontmatter.go` strips YAML frontmatter before goldmark parses. `title`, `timestamp`/`date`/`created` and `tags` become document-level fields; the useful ones are re-emitted as one leading section of plain prose so they stay searchable. AsciiDoc's `:name: value` attribute header is treated the same way (`internal/parser/adoc.go`): `:revdate:`/`:date:` becomes the timestamp, `:keywords:`/`:tags:` the tags.
- **Markup parsers keep source text**: `.rst` and `.adoc`/`.asciidoc` are parsed for structure only — headings become heading paths and the top title the document title — and body lines are copied verbatim through `lineSections` (`internal/parser/lines.go`), so every chunk cites exactly the raw lines it came from. rst title levels follow the order in which each adornment style first appears; transitions, table rules and `::` literal blocks are not titles. adoc headings inside delimited blocks are ignored and `////`/`//` comments are dropped. `.mdx` goes through the markdown parser after `blankMDXNoise` (`mdx.go`) overwrites top-level `import`/`export` paragraphs and JSX tag lines with spaces: offsets stay valid, and text inside `<TabItem>`-style blocks, which goldmark would otherwise swallow as an HTML block, gets indexed.
- **One result per document**: both retrievers keep only a document's best-ranked chunk — BM25 stops at `poolSize` *distinct documents* rather than applying a SQL `LIMIT` to chunks, and the vector KNN dedupes before truncating — and `ReciprocalRankFusion` keys on document ID, counting each document once at its best rank. Bounding the pool by chunks let one verbose file starve every other match.
- **Lean vector scan, late hydration**: the KNN scan (`internal/search/vector.go`) selects only `d.id`, `c.id`, `d.path` and the vector — identity, the scope glob's input, and the bytes the cosine math needs. Chunk text and document metadata are fetched afterwards by `hydrate`, for the selected top-K and their passages, with the IDs passed as one JSON array to `json_each` so an unbounded `--limit` cannot hit SQLite's bind-variable cap. Both reads share one `BeginTx` snapshot, since two statements would otherwise let a concurrent `qi index` change the corpus between ranking and hydration. Materializing `c.text` for every embedded chunk spent total corpus bytes on a snippet needed by at most `topK` rows; dropping it roughly halves scan time, though the scan still grows with chunk text because the line-range predicate reads columns stored after it. The connection pool holds one connection, so the scan rows must be closed before hydration runs.
- **Vector leg bounded by BM25 candidates**: hybrid search scores only the chunks of BM25's candidate documents (`VectorSearch.SearchDocs`, driven from `json_each` through `idx_chunks_doc_id`), not every embedded chunk. A full BM25 pool is the candidate set; a short one (a strict conjunction matching a few documents) is widened with the best documents matching *any* query term (`BM25.CandidateDocs`, `internal/search/candidates.go`, no snippets) up to the vector top-K. Only when BM25 finds nothing at all does the full-corpus scan still run. On a 424k-chunk corpus a hybrid `qi query` went from 1.41s to 0.01–0.06s; the no-lexical-match fallback stays at ~1.33s (a full scan, so roughly 8–12s at 2.7M chunks). The accepted cost: a document sharing no term with the query cannot surface through the vector leg — it re-ranks and fills from lexical candidates. Chosen over `ext/vec1` (see the vec1 note below).
- **Post-retrieval pass**: `search.Finalize` (`internal/search/postprocess.go`) applies date sort, duplicate-snippet collapse and a backstop one-per-document pass over the whole candidate pool, then truncates to the caller's limit. Retrievers no longer truncate; commands call `Finalize`.
- **Compaction on index**: every `qi index` hard-deletes deactivated documents, prunes orphaned content blobs, runs FTS5 `optimize` only once the segments written since the last one reach a quarter of the largest segment (read from the FTS5 structure record; `ftsSegmentPages`), and `VACUUM`s when the freelist exceeds a quarter of the file. Optimizing every run made a 10-file reindex pay for the whole corpus (1.2s → 0.05s at 424k chunks); automerge alone never reaches the bottom segment, so replaced postings pile up without the threshold. FTS5 `secure-delete` is on (migration 008), so a deleted or edited-away term leaves `chunks_fts_data` in the same run instead of lingering behind a delete marker until the next optimize — compaction exists so removed text leaves the database, and the threshold alone broke that for FTS. It costs about 0.25s on a 10-file run at 424k chunks. A restored file therefore re-chunks and re-embeds rather than reactivating.
- **Resource limits**: files over 10 MiB are skipped (`maxFileSize`, `internal/indexer`), counted in `skipped`, and a document whose file grew past the cap is deactivated. They used to fail the run, and one generated dump in a large tree then failed every run. A read failure never deactivates a good document, but the run names the paths whose stale content is still searchable and exits nonzero.
- **Stat skip on index**: `documents.file_stat` (migration 009) records the size and nanosecond mtime a document was indexed from. A file whose stat still matches is not read at all — no read, no SHA-256, no frontmatter re-parse — so a no-op run is a directory walk plus one indexed lookup per file: 9.2s → 2.7s on a 2.5M-chunk corpus, 2.1s → 0.9s at 424k. A changed stat with unchanged bytes just records the new stat. `--force` bypasses it for a writer that preserves both size and mtime.
- **Index runs serialize on a lock file**: `qi index` takes an flock on `<database>.index-lock` (`db.LockIndex`) before opening the database, and a second run waits for the first. Two pulls close together otherwise start two background runs, and any statement holding the write lock past the 10s busy timeout (VACUUM, a full FTS optimize at scale) failed the second one with "database is locked" at its migration lock, dropping its changes until the next pull. `--changed-since` is resolved to a SHA before waiting, because the next pull overwrites `ORIG_HEAD`; a revision that does not resolve (the first pull after a clone) prints that a full index is running instead of a failure.
- **BM25 snippets after ranking**: `searchFTS` ranks without `snippet()` and `addSnippets` fetches snippets for the selected chunks only, in the same `BeginTx` snapshot as the ranking and passages (IDs via `json_each`). SQLite materializes every matching row before the sorted output starts, so a snippet per match dominated common terms: at 2.5M chunks, `err` (412k matches) 5.2s → 1.2s, `the` 11.9s → 1.9s, byte-identical output. What remains is FTS5 scoring every match, which only bites single very common terms; multi-term queries intersect doclists and stay under 0.1s.
- **Pull hooks, not a watcher**: `qi hook install` writes identical `post-merge`, `post-rewrite` and `post-commit` hooks that `exec qi hook run <hook> "$@"`; all logic lives in Go (`planHookRun`, `cmd/hook.go`). `hook run` resolves the base to a SHA while git is still in the hook (a later pull or reset moves `ORIG_HEAD`): `ORIG_HEAD` after a merge or rebase pull, `HEAD^1` for a merge commit (a conflicted pull fires `post-commit`, never `post-merge`), none for a squash pull (HEAD does not move). It then spawns one detached `qi index --changed-since <sha> <paths...>` for every configured collection inside the worktree, so several doc collections in one monorepo share the hooks. Ordinary commits and amends exit in the hook script itself (a shell guard), without starting qi. Every git call goes through `gitOutput`, which always strips git's location variables. `docsChangedSince` reads `git diff --raw` (a submodule bump is a mode-160000 entry with no extension) and runs git with `GIT_DIR` and friends stripped (`gitEnv`), since a hook exports them and they re-root `git -C`. Install checks every hook before writing any and registers the collection only once nothing can fail; hooks call `qi` by its PATH location when that is the running binary, so a versioned install path is not pinned.
- **Auto-embed on index**: When an embedder is configured, chunks are embedded immediately after indexing without a separate step. Finding what to embed (`pendingChunks`) is pure SQL — missing rows, dimension, fingerprint and `length(vector)` — so a no-op run never reads a vector (4.7s → 1.85s at 424k chunks). Finite/non-zero values are enforced at write time only; a vector corrupted outside qi is skipped by search and counted as `corrupt` by `qi doctor`/`qi stats` (`EmbeddingHealth`), which point at `qi index --force` rather than a plain `qi index` that would not repair it.
- **Config**: Raw `gopkg.in/yaml.v3`, no viper. `~` expansion + relative path resolution.

## Package Structure

```
cmd/                  Cobra commands (root, init, index, search, query, get, doctor, stats, list, delete, update, hook)
internal/
  app/                Wires config + db + services
  config/             Config loading, defaults, path expansion
  db/                 SQLite open/migrate/WAL, embedding blob storage
    migrations/       Embedded SQL migrations, applied in filename order
  chunker/            Break-point chunker (chunker.Chunker interface)
  indexer/            Filesystem walker, SHA-256 change detection, embedder
  output/             Text/JSON formatters
  parser/             Document parsers (Markdown/MDX via goldmark, rst, AsciiDoc, plaintext)
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

A markup format that keeps its body as source text can build sections with `lineSections` (`internal/parser/lines.go`), which keeps every chunk's source line range exact.

## Adding a New Migration

Add `internal/db/migrations/00N_description.sql` — the runner applies them in alphabetical order and skips versions already recorded in `schema_version`. An `ALTER TABLE ... ADD COLUMN` migration must also register one added column in `addedColumns` (`migrate.go`), since SQLite has no `IF NOT EXISTS` for it.

## sqlite-vec Note

The plan called for `sqlite-vec` via `github.com/asg017/sqlite-vec-go-bindings/ncruces`. The bindings require `go-sqlite3 ≤ v0.17.1` (which uses wazero as WASM runtime), but the sqlite-vec WASM binary requires atomic instructions that wazero v1.7.3 doesn't enable by default. `go-sqlite3 ≥ v0.18` uses `wasm2go` (no wazero) and removed `sqlite3.Binary`. Until a compatible version of sqlite-vec-go-bindings is released, vector search uses pure Go KNN, bounded to BM25 candidates in hybrid search.

## vec1 Note

`github.com/ncruces/go-sqlite3/ext/vec1` (go-sqlite3 ≥ v0.35.5, IVF-ADC, CGo-free) was evaluated on the 424k-chunk, 768-dim benchmark and rejected for now. It builds and its index persists across processes. Queries against the IVF-only index (`codesize:0`, cosine) took about 30–60ms at nprobe 0.01–0.05. On clustered synthetic vectors, recall@10 against brute force was 1.0 for queries drawn from the data distribution, but only 0.03–0.28 for random queries. On the isotropic stub vectors it was 0.08 at nprobe 0.05 and 0.42 at nprobe 0.2. The blockers were operational:

- Keeping it in sync needs triggers on `chunk_vectors`, and vec1 is not an innocuous vtab: triggers fail with `unsafe use of virtual table` unless `PRAGMA trusted_schema=ON`. Once such triggers exist, any connection without vec1 registered (an older qi, the `sqlite3` CLI) fails every chunk delete with `no such module: vec1`.
- The vector size is fixed by the first insert and survives both emptying the table and a `rebuild` to `{index:"none"}`. A fingerprint change to a new dimension means dropping and recreating the table.
- Training runs on one core. The WASM build is `-ffreestanding -nostdlib`, has no pthread symbols, and `nthread` had no effect. Training took 30–57s on a 20k sample, and `rebuild` took another 2m35s at 424k vectors. OPQ on a 65k sample ran out of memory at the default 256MB limit (`sqlite3.WithMaxMemory` raises it). With 3GB it was still running after 10 minutes. Training also fails with no vectors, and it has no natural home outside `qi index`.
- It stores a second copy of every vector: the database grew by 1.66GB, from 2.17GB to 3.83GB.

Revisit vec1 if it gains threads in WASM or innocuous-vtab status, or if semantic-only recall is needed.
