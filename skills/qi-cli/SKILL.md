---
name: qi-cli
description: Guide for using qi, a local knowledge search CLI for macOS and Linux. Use this skill whenever the user asks about qi commands, indexing documents, searching a knowledge base, asking questions with RAG, configuring providers (Ollama, OpenAI), understanding search modes (BM25, hybrid, vector), or anything related to the qi tool. Triggers on phrases like "qi index", "qi search", "qi ask", "qi query", "how do I use qi", "set up qi", "configure qi", "search my notes with qi", or any mention of the qi CLI.
---

qi is a local-first knowledge search CLI. It indexes documents into SQLite and supports BM25 full-text search, vector/hybrid search (with a local or remote embedding provider), and LLM-powered Q&A with citations.

## Quick Start

```bash
qi init                                   # Create config + database
$EDITOR ~/.config/qi/config.yaml         # Add collections and (optionally) providers
qi index                                  # Index the current directory, or a named collection
qi doctor                                 # Verify setup
qi search "your query"                    # BM25 keyword search (no provider needed)
qi query "your semantic question"         # Hybrid search (needs embedding provider)
qi ask "what does X do?"                  # RAG Q&A (needs generation provider)
```

---

## Commands

### `qi init`
Writes `~/.config/qi/config.yaml` (if absent) and initializes the SQLite database.

```bash
qi init
```

### `qi index [path|collection]`
Indexes documents. SHA-256 content hashing means unchanged files are skipped.

```bash
qi index                              # indexes current working directory
qi index ~/notes                      # any absolute or relative path
qi index notes                        # named collection from config

# directories are auto-named from their path on first run:
# ~/Projects/tools/qi → "Projects-tools-qi"
qi index                              # indexes and auto-names current directory
qi index ~/notes                      # indexes and auto-names ~/notes

# --name overrides the auto-generated name with a custom one
qi index ~/notes --name notes         # save + index ~/notes as "notes"
qi index --name notes                 # save + index current directory as "notes"
```

### `qi search <query>`
BM25 full-text search. Fast, offline, no provider needed.

```bash
qi search "authentication"
qi search "deploy" -c code -n 5
```

### `qi query <query>`
Semantic/hybrid search. Falls back gracefully to BM25 if the embedding provider is unavailable.

```bash
qi query "how does auth work"
qi query "deploy pipeline" --mode lexical   # BM25 only
qi query "deployment steps" --mode hybrid   # BM25 + vector (default)
qi query "critical path" --mode deep        # hybrid + reranking
qi query "question" --explain               # show BM25/vector/RRF score breakdown
```

**Modes:**
- `lexical` — BM25 only
- `hybrid` (default) — BM25 + vector fused with RRF; skips vector if BM25 has a clear winner
- `deep` — hybrid + optional reranking pass

### `qi ask <question>`
RAG Q&A: searches the knowledge base, sends relevant chunks to an LLM, returns an answer with citations.

```bash
qi ask "What authentication methods are supported?"
qi ask "Explain the chunking algorithm" -c code
```

Requires a `generation` provider in config.

### `qi get <id>`
Retrieve a document by its 6-character hash prefix (shown in search results).

```bash
qi get abc123
```

### `qi list`
List all named collections defined in config (name and path).

```bash
qi list
```

### `qi delete <collection>`
Delete a named collection: removes all indexed data from the database and removes the collection entry from config. Irreversible.

```bash
qi delete notes
```

### `qi stats`
Show document counts, chunk counts, embedding counts, and database size per collection.

### `qi doctor`
Health-check config, database, collection paths, and provider connectivity.

### `qi update`
Update the binary from GitHub. If installed via Homebrew, it suggests `brew upgrade qi` instead.

---

## Global Flags

| Flag | Description |
|---|---|
| `-v, --verbose` | Verbose/debug output |
| `-f, --format text\|json\|markdown` | Output format (default: text) |
| `--config <path>` | Override config path |
| `-c, --collection <name>` | Limit to a specific collection |
| `-n, --limit <N>` | Number of results (default: 10) |

---

## Configuration (`~/.config/qi/config.yaml`)

```yaml
database_path: ~/.local/share/qi/qi.db

collections:
  - name: notes
    path: ~/notes
    extensions: [.md, .txt]
    ignore: [.git]

  - name: code
    path: ~/Projects/myproject
    extensions: [.go, .ts, .py]
    ignore: [vendor, dist]

providers:
  embedding:                              # optional — enables vector/hybrid search
    base_url: http://localhost:11434      # Ollama, LM Studio, llama.cpp, OpenAI-compatible
    model: nomic-embed-text
    dimension: 768

  rerank:                                 # optional — enables deep mode
    base_url: http://localhost:8080
    model: bge-reranker-v2-m3

  generation:                             # optional — enables `qi ask`
    base_url: http://localhost:11434
    model: llama3.2
    api_key: ""                           # set for OpenAI or auth-gated services

search:
  default_mode: hybrid                    # lexical | hybrid | deep
  bm25_top_k: 50
  vector_top_k: 50
  rerank_top_k: 10
  rrf_k: 60
  chunk_size: 512
```

### Common provider setups

**Ollama (fully local):**
```yaml
providers:
  embedding:
    base_url: http://localhost:11434
    model: nomic-embed-text
    dimension: 768
  generation:
    base_url: http://localhost:11434
    model: llama3.2
```

**OpenAI:**
```yaml
providers:
  embedding:
    base_url: https://api.openai.com/v1
    model: text-embedding-3-small
    dimension: 1536
  generation:
    base_url: https://api.openai.com/v1
    model: gpt-4o
    api_key: sk-...
```

**Mixed (local embeddings, cloud generation):**
```yaml
providers:
  embedding:
    base_url: http://localhost:11434
    model: nomic-embed-text
    dimension: 768
  generation:
    base_url: https://api.openai.com/v1
    model: gpt-4o
    api_key: sk-...
```

---

## How search works

- **BM25** — SQLite FTS5. Always available, very fast, good for keyword queries.
- **Vector KNN** — Cosine similarity over embedding BLOBs in SQLite. Requires an embedding provider. Captures semantic intent.
- **Hybrid (RRF)** — Runs both, fuses rankings with Reciprocal Rank Fusion (`score = Σ 1/(k + rank)`). Skips vector if BM25 has a dominant winner (top score > 3× second place).
- **Deep** — Hybrid + a second-pass reranker for best accuracy.

---

## Document references

Search results show locations like `qi://notes/2024/jan.md [Section > Subsection]` and a 6-character ID. Use `qi get <id>` to view the full document.

---

## Typical workflows

**Index and search (no provider needed):**
```bash
qi init
qi index ~/notes                 # auto-named "notes" on first run
qi search "my keyword" -c notes
```

**Manage named collections:**
```bash
qi list                          # see all configured collections
qi index ~/projects              # auto-named "projects" on first run
qi delete projects               # remove collection data + config entry
```

**Semantic search with Ollama:**
```bash
# pull a model: ollama pull nomic-embed-text
# add embedding provider to config
qi index notes
qi query "how does X work" --explain
```

**RAG Q&A:**
```bash
# also add a generation provider to config
qi ask "Summarize the key decisions in my notes"
```

**Debug / inspect:**
```bash
qi doctor                         # check all providers are reachable
qi stats                          # see document/chunk/embedding counts
qi query "question" --explain     # see score breakdown
qi get abc123                     # read the full source document
```
