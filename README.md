# qi - query search engine for ai agents

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Claude](https://img.shields.io/badge/Claude-D97757?logo=claude&logoColor=fff)](https://claude.ai/code)
[![Releases](https://img.shields.io/github/v/release/itsmostafa/qi)](https://github.com/itsmostafa/qi/releases)

<p align="center">
  <img src="assets/img/qi-logo.png" alt="qi logo" width="200" />
</p>

A local-first knowledge search CLI for macOS. Index your documents and search them using BM25 full-text search, vector embeddings, and LLM-powered Q&A — all running locally with no external dependencies.

## Quickstart

```sh
# Install
go install github.com/itsmostafa/qi@latest

# Initialize config and database
qi init

# Edit config to point at your documents
$EDITOR ~/.config/qi/config.yaml

# Index your documents
qi index

# Search
qi search "my query"

# Hybrid search (BM25 + vector, requires embedding provider)
qi query "my query" --mode hybrid

# Ask a question (requires generation provider)
qi ask "how does X work?"

# Health check
qi doctor
```

## Configuration

The config lives at `~/.config/qi/config.yaml`. See [`config.example.yaml`](config.example.yaml) for a fully annotated example.

```yaml
database_path: ~/.local/share/qi/qi.db

collections:
  - name: notes
    path: ~/notes
    extensions: [.md, .txt]

providers:
  embedding:
    base_url: http://localhost:11434  # Ollama
    model: nomic-embed-text
    dimension: 768

  generation:
    base_url: http://localhost:11434
    model: llama3.2
```

## Commands

| Command | Description |
|---|---|
| `qi init` | Create config and database |
| `qi index [collection]` | Index all (or one) collection |
| `qi search <query>` | BM25 full-text search |
| `qi query <query>` | Hybrid search (BM25 + vector) |
| `qi ask <question>` | RAG-powered answer with citations |
| `qi get <id>` | Retrieve document by 6-char hash ID |
| `qi stats` | Show index statistics |
| `qi doctor` | Health check |
| `qi version` | Print version |

## Search Modes

`qi query` supports three modes via `--mode`:

- **`lexical`**: BM25 full-text search only
- **`hybrid`** (default): BM25 + vector search fused with Reciprocal Rank Fusion (RRF)
- **`deep`**: hybrid + optional reranking

Use `--explain` to see scoring breakdown:

```sh
qi query "chunking algorithm" --mode hybrid --explain
```

## Architecture

- **Storage**: SQLite with content-addressable blobs (SHA-256 keyed `content` table), FTS5 for BM25, BLOB-stored embeddings for vector KNN search
- **Chunking**: Break-point scoring (headings=100, code fences=80, blank lines=20) with distance decay from target chunk size
- **Providers**: OpenAI-compatible HTTP API adapters (Ollama, llama.cpp, etc.)
- **Graceful degradation**: Vector search and Q&A are optional — BM25 always works

## Document IDs

Each document gets a short ID from the first 6 hex characters of its SHA-256 content hash:

```sh
qi get abc123
```

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
