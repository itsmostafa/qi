# qi - query search engine cli for ai agents and humans

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Claude](https://img.shields.io/badge/Claude-D97757?logo=claude&logoColor=fff)](https://claude.ai/code)
[![Releases](https://img.shields.io/github/v/release/itsmostafa/qi)](https://github.com/itsmostafa/qi/releases)

<p align="center">
  <img src="assets/img/qi-logo.png" alt="qi logo" width="200" />
</p>

A local-first knowledge search CLI for macOS and Linux. Index your documents and search them using BM25 full-text search, vector embeddings, and LLM-powered Q&A — all running locally with no external dependencies.

## Features

- **Blazing-fast full-text search** — BM25 via SQLite FTS5, no external search engine required
- **Vector search that stays local** — embeddings stored and queried entirely on your machine; works with Ollama, LM Studio, llama.cpp, or any OpenAI-compatible provider
- **Hybrid search with RRF fusion** — combines BM25 and vector rankings for results that are both precise and semantically aware
- **LLM-powered Q&A with citations** — ask questions in plain English and get grounded answers pointing back to your actual documents
- **Smart chunking** — breakpoint scoring prioritizes headings, code fences, and paragraph boundaries so chunks stay meaningful, not arbitrary
- **Zero-dependency storage** — a single SQLite file holds your entire index; content-addressable blobs (SHA-256) eliminate duplicates automatically
- **Works offline, always** — vector search and Q&A are optional enhancements; BM25 search works out of the box with no providers configured

## Install

```sh
brew tap itsmostafa/qi
brew install qi
```

Or via `go install`:

```sh
go install github.com/itsmostafa/qi@latest
```

## Quickstart

```sh
# Initialize config and database
qi init

# Index current directory
qi index

# Or index a specific path
qi index ~/notes

# Or index a named collection from config
$EDITOR ~/.config/qi/config.yaml  # Configure collections
qi index notes

# Search
qi search "my query"

# Hybrid search (BM25 + vector, requires embedding provider)
qi query "my query" --mode hybrid

# Ask a question (requires generation provider)
qi ask "how does X work?"

# Health check
qi doctor
```

## Commands

| Command | Description |
|---|---|
| `qi init` | Create config and database |
| `qi index [path\|collection]` | Index directory (current dir by default) or named collection |
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

## Document IDs

Each document gets a short ID from the first 6 hex characters of its SHA-256 content hash:

```sh
qi get abc123
```

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
