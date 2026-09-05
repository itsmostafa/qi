---
name: qi-cli
description: Guide for using qi, a local knowledge search CLI for macOS and Linux. Use this skill when the user wants to index documents into a searchable knowledge base, search or retrieve from one, configure qi's collections or embedding providers (Ollama, OpenAI-compatible), choose between its search modes (BM25, hybrid, vector), or diagnose why qi search results look wrong — or on any mention of the qi CLI.
---

qi is a local-first knowledge search CLI. It indexes documents into SQLite and supports BM25 full-text search and vector/hybrid search (with a local or remote embedding provider).

## Quick Start

```bash
qi init                                   # Create config + database
$EDITOR ~/.config/qi/config.yaml         # Add collections and (optionally) providers
qi index                                  # Index the current directory, or a collection
qi doctor                                 # Verify setup
qi search "your query"                    # BM25 keyword search (no provider needed)
qi query "your semantic question"         # Hybrid search (needs embedding provider)
```

---

## Commands

### `qi init`
Writes `~/.config/qi/config.yaml` (if absent) and initializes the SQLite database.

```bash
qi init
```

### `qi index [path|collection]`
Indexes documents. SHA-256 content hashing means unchanged files are skipped. Embeddings are generated at index time — if you add an embedding provider to config after an initial index run, re-run `qi index` to populate the missing embeddings.

```bash
qi index                              # indexes current working directory
qi index ~/notes                      # any absolute or relative path
qi index notes                        # generated collection name from config
```

### `qi search <query>`
BM25 full-text search. Fast, offline, no provider needed.

```bash
qi search "authentication"
qi search "deploy" -c wiki -n 5
```

### `qi query <query>`
Semantic/hybrid search. Falls back gracefully to BM25 if the embedding provider is unavailable.

```bash
qi query "how does auth work"
qi query "deploy pipeline" --mode lexical   # BM25 only
qi query "deployment steps" --mode hybrid   # BM25 + vector (default)
qi query "critical path" --mode deep        # alias for hybrid
qi query "question" --explain               # show BM25/vector/RRF score breakdown
```

**Modes:**
- `lexical` — BM25 only
- `hybrid` — BM25 + vector fused with RRF; skips vector if BM25 has a clear winner
- `deep` — alias for `hybrid`

With no `--mode`, the mode comes from `search.default_mode` in the config (`hybrid` by default).

### `qi get <id>`
Retrieve a document by its 6-character hash prefix (shown in search results). An
ambiguous prefix is an error listing the candidates, never a dump of all of them.

```bash
qi get abc123
qi get abc123 --lines 40:80      # 1-indexed, inclusive; "40:" and ":80" also work
qi get abc123 --max-bytes 4096   # truncate and say so (0 = unlimited)
qi get abc123 --format json      # {collection, path, title, hash, body, truncated}
```

Prefer `--lines`/`--max-bytes` over reading whole documents: they are what keeps
a retrieved document from swallowing an agent's context.

### `qi list`
List all collections defined in config (name and path).

```bash
qi list
```

### `qi delete <collection>`
Delete a collection: removes all indexed data from the database and removes the collection entry from config. Irreversible.

```bash
qi delete notes
```

### `qi stats`
Show document counts, chunk counts, embedding counts, and database size per collection. If `Embeddings` count is much lower than `Chunks` count, vector search has no data to work with — re-run `qi index`.

### `qi doctor`
Health-check config, database, collection paths, and embedding coverage. It does **not** contact the provider: a `SKIP` line means no embedding provider is configured, not that one is unreachable. When none is configured, `qi query` silently falls back to BM25.

### `qi update`
Update the binary in place from the latest GitHub release.

---

## Global Flags

| Flag | Description |
|---|---|
| `--verbose` | Log debug detail to stderr (no `-v` shorthand; `-v` is `--version` on `qi` itself) |
| `-f, --format text\|json\|markdown` | Output format (default: text) |
| `--config <path>` | Override config path |

## `search` and `query` Flags

These are per-command, not global.

| Flag | Description |
|---|---|
| `-c, --collection <name>` | Limit to a specific collection |
| `-n, --limit <N>` | Number of results (default: 10) |
| `--since YYYY-MM-DD` | Only documents dated on or after this day |
| `--until YYYY-MM-DD` | Only documents dated on or before this day |
| `--sort date` | Newest first instead of by relevance |

Every document has a date. It comes from the frontmatter `timestamp:`, `date:`
or `created:` when one of them parses, otherwise from the file's modification
time — so no document is excluded from `--since`/`--until` for lack of a date.

---

## Configuration (`~/.config/qi/config.yaml`)

```yaml
database_path: ~/.local/share/qi/qi.db

collections:
  - name: notes
    path: ~/notes
    extensions: [.md, .txt]
    ignore: [.git]

  - name: wiki
    path: ~/wiki
    extensions: [.md, .txt]
    ignore: [.git]

providers:
  embedding:                              # optional — enables vector/hybrid search
    base_url: http://localhost:11434      # Ollama, LM Studio, llama.cpp, OpenAI-compatible
    model: nomic-embed-text
    dimension: 768
    max_input_chars: 0                    # optional — truncate long texts before embedding (0 = no limit)

search:
  default_mode: hybrid                    # lexical | hybrid (deep is an alias for hybrid)
  bm25_top_k: 50
  vector_top_k: 50
  rrf_k: 60
  chunk_size: 512
  prefer_extensions: [.md, .txt]          # optional — boost results with these extensions
  extension_boost: 2.0                     # optional — score multiplier for preferred extensions (default 2.0)
```

`base_url` and `api_key` support `${VAR}` environment-variable expansion, and a trailing `/v1` in `base_url` is normalized automatically (no doubled `/v1/v1`).

### Common provider setups

**Ollama (fully local):**
```yaml
providers:
  embedding:
    base_url: http://localhost:11434
    model: nomic-embed-text
    dimension: 768
```

**OpenAI:**
```yaml
providers:
  embedding:
    base_url: https://api.openai.com/v1
    model: text-embedding-3-small
    dimension: 1536
    api_key: sk-...
```

---

## How search works

- **BM25** — SQLite FTS5. Always available, very fast, good for keyword queries.
- **Vector KNN** — Cosine similarity over embedding BLOBs in SQLite. Requires an embedding provider. Captures semantic intent.
- **Hybrid (RRF)** — Runs both, fuses rankings with Reciprocal Rank Fusion (`score = Σ 1/(k + rank)`). Skips vector if BM25 has a dominant winner (top score > 3× second place).
- **Deep** — Accepted as an alias of Hybrid.

---

## Document references

Search results show locations like `qi://notes/2024/jan.md [Section > Subsection]` and a 6-character ID. Use `qi get <id>` to view the full document.

---

## Typical workflows

**Index and search (no provider needed):**
```bash
qi init
qi index ~/notes                 # saves and indexes ~/notes as "notes"
qi search "my keyword" -c notes
```

**Manage collections:**
```bash
qi list                          # see all configured collections
qi index ~/projects              # add a new collection
qi delete old-notes              # remove collection data + config entry
```

**Semantic search with Ollama:**
```bash
# pull a model: ollama pull nomic-embed-text
# add embedding provider to config
qi index notes
qi query "how does X work" --explain
```

**Debug / inspect:**
```bash
qi doctor                         # check config, database, and embedding coverage
qi stats                          # see document/chunk/embedding counts
qi query "question" --explain     # see score breakdown
qi get abc123                     # read the full source document
```

---

## Troubleshooting

### `qi query` returns keyword-only results despite embedding provider being configured

Work through these steps in order:

**1. Check if the provider is actually wired up**
```bash
qi doctor
```
Look for `OK` next to the embedding provider. `SKIP` means qi didn't parse the provider config — common causes: wrong YAML key name (`embedding` not `embeddings`), missing `dimension` field, or bad indentation (must be under `providers:`).

**2. Check if embeddings were actually generated**
```bash
qi stats
```
If `Embeddings` is 0 or far less than `Chunks`, the vectors were never written. This happens when the embedding provider was added to config *after* the initial `qi index` run — re-index to fix it:
```bash
qi index <collection-name>
```
Only chunks missing embeddings are re-processed; unchanged files aren't re-parsed.

**3. Verify the provider is reachable at query time**
`qi query` embeds the query string at runtime. If the provider is down, qi silently falls back to BM25 without an error. Test connectivity:
```bash
# Ollama:
curl http://localhost:11434/v1/embeddings -H "Content-Type: application/json" \
  -d '{"model":"nomic-embed-text","input":["test"]}'
```
Also confirm the model is pulled: `ollama list`.

**4. Use `--explain` to see what's actually happening**
```bash
qi query "your question" --explain
```
This shows each result's BM25 rank, vector rank, and fused RRF score. If every result is missing a vector rank, the vector path isn't contributing — confirming one of the issues above.

**Note:** hybrid mode also skips vector search if the top BM25 score is more than 3× the second result (intentional optimization for dominant keyword matches). If your queries are exact keyword matches, try a more paraphrased, natural-language query to avoid this shortcut.
