# qi - query engine cli for ai agents and humans

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Claude](https://img.shields.io/badge/Claude-D97757?logo=claude&logoColor=fff)](https://claude.ai/code)
[![Releases](https://img.shields.io/github/v/release/itsmostafa/qi)](https://github.com/itsmostafa/qi/releases)

<p align="center">
  <img src="assets/img/qi-logo.png" alt="qi logo" width="200" />
</p>

Save tokens by delegating some of your AI Agent's work to qi. Agent skills included.

- ⚡ Ultra-fast indexing
- ⚡ Lower tokens + latency
- 🧠 Better reasoning (agents focus on thinking, not retrieval)
- 🔒 Fully local + offline
- 🧩 Works with Ollama, LM Studio, Claude, OpenAI, MLX, etc.


## Features

- **Blazing-fast full-text search** — BM25 via SQLite FTS5, no external search engine required
- **Flexible vector search** — embeddings stored and queried on your machine; works with Ollama, LM Studio, llama.cpp, or OpenAI's SOTA models.
- **Hybrid search with RRF fusion** — combines BM25 and vector rankings for results that are both precise and semantically aware
- **Smart chunking** — breakpoint scoring prioritizes headings, code fences, and paragraph boundaries so chunks stay meaningful, not arbitrary
- **Zero-dependency storage** — a single SQLite file holds your entire index; content-addressable blobs (SHA-256) eliminate duplicates automatically
- **Works offline, always** — vector search is an optional enhancement; BM25 search works out of the box with no providers configured

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/itsmostafa/qi/main/install.sh | sh
```

The script detects your OS and architecture, and installs the
latest release.
Run `qi update` later to upgrade in place.

Or via `go install`:

```sh
go install github.com/itsmostafa/qi@latest
```

### Claude Code Plugin

qi is available as a Claude Code plugin. Add the marketplace and install with:

```
# Add the marketplace
/plugin marketplace add itsmostafa/qi

# Install the plugin
/plugin install qi
```

## Quickstart

```sh
# Initialize config and database
qi init

# Index current directory
qi index

# Or index a specific path
qi index ~/notes

# Collection names are generated from paths
# ~/notes -> notes

# Re-index it later by generated collection name
qi index notes

# Search
qi search "my query"

# Search a specific collection
qi search "my query" -c notes

# Hybrid search (BM25 + vector, requires embedding provider)
qi query "my query" --mode hybrid

# Hybrid search a specific collection
qi query "my query" --mode hybrid -c notes

# List all collections
qi list

# Delete a collection and all its indexed data
qi delete notes

# Health check
qi doctor
```

## Commands

| Command | Description |
|---|---|
| `qi init` | Create config and database |
| `qi index [path\|collection]` | Index directory (current dir by default) or collection |
| `qi search <query>` | BM25 full-text search |
| `qi query <query>` | Hybrid search (BM25 + vector) |
| `qi get <id>` | Retrieve document by 6-char hash ID (`--lines A:B`, `--max-bytes N`) |
| `qi list` | List all collections |
| `qi delete <collection>` | Delete a collection and all its indexed data |
| `qi stats` | Show index statistics |
| `qi doctor` | Health check |

## Search Modes

`qi query` supports three modes via `--mode`, defaulting to `search.default_mode` from the config:

- **`lexical`**: BM25 full-text search only
- **`hybrid`** (default): BM25 + vector search fused with Reciprocal Rank Fusion (RRF)
- **`deep`**: alias for `hybrid`

Use `--explain` to see scoring breakdown:

```sh
qi query "chunking algorithm" --mode hybrid --explain
```

## Documentation

Full documentation is in the [`docs/`](docs/) directory:

- [`docs/architecture.md`](docs/architecture.md) — system architecture, data flows, and design decisions
- [`docs/configuration.md`](docs/configuration.md) — all config options with explanations
- [`docs/config.example.yaml`](docs/config.example.yaml) — fully annotated example config
- [`docs/named-collections.md`](docs/named-collections.md) — collections guide

## Configuration

The config lives at `~/.config/qi/config.yaml`. See [`docs/configuration.md`](docs/configuration.md) for all options or [`docs/config.example.yaml`](docs/config.example.yaml) for a fully annotated example.

```yaml
database_path: ~/.local/share/qi/qi.db

collections:
  - name: notes
    path: ~/notes
    extensions: [.md, .txt]

providers:
  # Local (Ollama / llama.cpp)
  embedding:
    name: ollama
    base_url: http://localhost:11434
    model: nomic-embed-text
    dimension: 768

  # Or: OpenAI cloud (set OPENAI_API_KEY in your environment)
  # embedding:
  #   name: openai
  #   model: text-embedding-3-small
  #   dimension: 1536
  #   batch_size: 32
```

## Document IDs

Each document gets a short ID from the first 6 hex characters of its SHA-256 content hash:

```sh
qi get abc123
qi get abc123 --lines 40:80      # 1-indexed, inclusive
qi get abc123 --max-bytes 4096   # truncate long documents
```

A prefix matching more than one document is an error listing the candidates.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
