# qi - query engine cli for ai agents and humans

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Claude](https://img.shields.io/badge/Claude-D97757?logo=claude&logoColor=fff)](https://claude.ai/code)
[![Releases](https://img.shields.io/github/v/release/itsmostafa/qi)](https://github.com/itsmostafa/qi/releases)

<p align="center">
  <img src="assets/img/qi-logo.png" alt="qi logo" width="200" />
</p>

qi indexes your notes and docs into one local SQLite file and answers searches with the exact file and line range of each match, so you and your AI agents can find and cite the right passage without reading whole files. It runs on macOS and Linux, offline.

```console
$ qi search "rollback"
1. Deploy runbook (bm25: 0.0000) [2026-09-01]
   qi://notes/runbook.md#L9-L9 [Deploy runbook > Rollback]
   qi get 92256d662922d88001924aa57cd56370da30237c5d8d9a870fcd0c9efc347486 --lines 9:9
   To roll back a deploy, run make rollback and confirm the previous tag.
```

## Why qi

An agent that answers questions about your notes usually greps the tree and then reads every file that matched. Each file it opens costs tokens and a round trip, and most of what it reads is irrelevant.

qi does the retrieval up front. `qi index` splits each document into chunks at headings, code fences and paragraph breaks, and stores them in a SQLite database. A search returns one ranked hit per document, each with a snippet, its heading path and a line range. The agent then fetches only those lines with `qi get`. The repository ships an [agent skill](skills/qi-cli/SKILL.md) and a Claude Code plugin that teach an agent this workflow.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/itsmostafa/qi/main/install.sh | sh
```

The script downloads the latest release for macOS or Linux (amd64 or arm64), checks it against the release's SHA-256 checksums, and installs it to `~/.local/bin`. Set `QI_INSTALL_DIR` to install somewhere else. If that directory is not on your `PATH`, the script says so.

Run `qi update` later to replace the binary with the latest release. If its directory is not writable, run `sudo qi update`.

With a Go toolchain you can build from source instead:

```sh
go install github.com/itsmostafa/qi@latest
```

### Claude Code plugin

```
/plugin marketplace add itsmostafa/qi
/plugin install qi
```

## Quickstart

```sh
qi init                 # write ~/.config/qi/config.yaml and create the database
qi index ~/notes        # index a directory; it becomes the collection "notes"
qi search "rollback"    # keyword search across every collection
```

`qi init` writes a sample collection pointing at `~/notes`. Edit or delete it if you keep your notes elsewhere; `qi doctor` reports a failed check for any collection whose path does not exist.

Then try:

```sh
qi index                                   # index the current directory
qi index notes                             # re-index a collection by name
qi search "rollback" -c notes              # search one collection
qi search "rollback" --path 'meetings/*'   # only paths matching a glob
qi search "rollback" --since 2026-09-01    # only documents dated on or after
qi search "rollback" -f json               # JSON output for scripts and agents
qi query "how do I undo a release"         # hybrid search, needs an embedding provider
qi hook install ~/src/monorepo             # re-index after every git pull
```

## How search works

qi has two ways to rank chunks and a third that combines them.

- **Keyword search (BM25).** BM25 is the standard keyword-ranking formula: a chunk scores higher when it contains your terms often and when those terms are rare across the corpus. qi uses SQLite's built-in FTS5 full-text index for it. It needs no setup. If every term must match and nothing does, qi retries with any term matching.
- **Vector search.** An embedding model turns each chunk and your query into a list of numbers that encodes meaning, and qi ranks chunks by how close their vectors are to the query's. Two passages can match this way with no words in common, such as "undo a release" and "roll back a deploy". It needs an embedding provider (see [Configuration](#configuration)).
- **Hybrid search.** `qi query` runs both and merges the two rankings with Reciprocal Rank Fusion (RRF), which scores each document by its rank in each list, so a document near the top of either list ranks well. To stay fast, the vector side scores only the documents keyword search found (topped up with documents matching any query term when that list is short), and scans every chunk only when no document matches any term. On a 424,000-chunk test corpus that took a hybrid query from 1.41 seconds to 0.01 to 0.06 seconds. The cost: a document with no query term in it cannot surface through vector search.

`qi search` is always keyword search. `qi query` takes `--mode lexical` or `--mode hybrid` (`deep` is an alias for `hybrid`); the default comes from `search.default_mode` in the config, which is `hybrid`. Without an embedding provider, `qi query` prints a warning and falls back to keyword search. Add `--explain` to see each hit's BM25 rank, vector rank and RRF score:

```sh
qi query "chunking algorithm" --explain
```

## What qi gives you

- **Cited results.** Every hit carries a SHA-256 content hash, a `qi://collection/path` URI and a 1-indexed line range, so an answer can point at its source.
- **One result per document.** A long file with many matching chunks takes one slot, at its best chunk, instead of crowding out other documents. `--passages N` (0 to 5) adds up to N more matching passages under that hit.
- **Date filters.** `--since`, `--until` and `--sort date` use the document's frontmatter date (`date`, `timestamp` or `created`), or the file's modification time when there is none.
- **Fast re-indexing.** Files whose size and modification time have not changed are not read again. On a 424,000-chunk corpus a re-index with no changes took 0.9 seconds.
- **Formats.** Markdown (`.md`, `.markdown`), MDX (`.mdx`), reStructuredText (`.rst`), AsciiDoc (`.adoc`, `.asciidoc`) and plain text (`.txt`, `.text`). Headings become each chunk's heading path. YAML frontmatter and AsciiDoc header attributes supply the title, date and tags.
- **Indexing on git pull.** `qi hook install` adds git hooks that start a background `qi index` after a pull that changed indexed files, for every collection in that repository.
- **Local and offline.** The index is one SQLite file. Keyword search needs nothing else. Vector search works with any embedding server that speaks the OpenAI `/v1/embeddings` API, including local ones such as Ollama, llama.cpp and LM Studio.

## Commands

| Command | Description |
|---|---|
| `qi init` | Write a default config (if none exists) and create the database |
| `qi index [path\|collection]...` | Index directories or configured collections (current directory by default). `--force` rebuilds unchanged files, which you need after an upgrade that changes parsing |
| `qi search <query>` | Keyword (BM25) search |
| `qi query <query>` | Hybrid or keyword search (`--mode`, `--explain`) |
| `qi get <hash>` | Print an indexed document by content hash or unambiguous prefix (`--lines A:B`, `--max-bytes N`) |
| `qi list` | List collections |
| `qi delete <collection>` | Remove a collection from the config and delete its indexed data |
| `qi stats` | Documents, chunks and embedding coverage per collection, plus database size |
| `qi doctor` | Check the config, database, collection paths and embedding coverage |
| `qi hook install [path\|collection]` | Install git hooks that re-index a repository's collections after every pull |
| `qi update` | Replace qi with the latest GitHub release |

`search` and `query` share these flags: `-c/--collection`, `-n/--limit` (default 10), `--path <glob>`, `--since YYYY-MM-DD`, `--until YYYY-MM-DD`, `--sort relevance|date` and `--passages N`. Every command takes `-f/--format text|json|markdown` and `--config <file>`.

## Collections

A collection is a directory qi indexes. Its name is the directory's own name: `~/notes` becomes `notes`, and `/Users/alice/Projects/tools/qi` becomes `qi`. When two collections would share a name, each takes on leading path segments until the names differ, so `~/work/notes` and `~/personal/notes` become `work-notes` and `personal-notes`. Indexing a new path saves it to the config as a collection.

qi always skips common tool and build directories (`.git`, `node_modules`, `vendor`, `target`, `dist`, `build`, `.venv` and others), files over 10 MiB, and binary files. A collection's `ignore` list adds glob patterns to skip.

See [`docs/named-collections.md`](docs/named-collections.md) for more.

## Configuration

The config lives at `~/.config/qi/config.yaml` (or `$XDG_CONFIG_HOME/qi/config.yaml`) and the database at `~/.local/share/qi/qi.db` (or `$XDG_DATA_HOME/qi/qi.db`). Paths may start with `~`, and relative paths resolve against the config file's directory.

```yaml
database_path: ~/.local/share/qi/qi.db

collections:
  - path: ~/notes
    # Without this list qi indexes .md, .markdown, .txt and .text only.
    # List .mdx, .rst, .adoc or .asciidoc here to index those formats.
    extensions: [.md, .mdx, .rst, .adoc, .txt]
    ignore: [drafts, "*.tmp"]

providers:
  # A local Ollama server
  embedding:
    name: ollama
    base_url: http://localhost:11434
    model: nomic-embed-text
    dimension: 768

  # Or OpenAI: base_url and api_key default to https://api.openai.com
  # and the OPENAI_API_KEY environment variable.
  # embedding:
  #   name: openai
  #   model: text-embedding-3-small
  #   dimension: 1536
```

With an embedding provider configured, `qi index` embeds new and changed chunks in the same run, 32 per request unless you set `batch_size`. `base_url` and `api_key` can reference environment variables as `${VAR}`.

- [`docs/configuration.md`](docs/configuration.md): every option, with examples for Ollama, llama.cpp, LM Studio, OpenAI and other compatible servers
- [`docs/config.example.yaml`](docs/config.example.yaml): an annotated example config
- [`docs/architecture.md`](docs/architecture.md): system architecture, data flow and design decisions

## Retrieving and citing search hits

Every `search` and `query` hit includes a full SHA-256 `hash`, a `source_uri` identifying its collection and path, and 1-indexed, inclusive `start_line` and `end_line` values in the original indexed source. The hash identifies the version of the source; cite the URI, hash and line range together. The integer `doc_id` and `chunk_id` fields are internal IDs, not arguments to `get`. If you pass `--config`, use the same config for search and retrieval.

```sh
qi search "deployment" --format json
# Copy hash, start_line and end_line from a hit:
qi get <hash> --lines <start_line>:<end_line>
qi get <hash> --max-bytes 4096

# Include up to two more matching passages per document:
qi query "deployment" --passages 2 --format json
```

Extra passages do not take result slots or add ranking weight. They inherit the parent hit's hash and source URI. Line ranges cover the source text behind each chunk, not only the highlighted words, so Markdown syntax and frontmatter are present when you retrieve them.

`get` reads the **indexed snapshot**, not the live file. Editing a file leaves the snapshot unchanged until you re-index. qi does not keep old versions: once a file is replaced or deleted and the index is compacted, its old hash no longer resolves. A hash alone does not prove the current file is unchanged.

Unambiguous hash prefixes such as `abc123` also work; a prefix that matches more than one content hash is an error. Identical content at several paths is returned once, with the other locations in `also_at`, so keep the search hit's `source_uri` as the citation.

Search skips chunks with missing or invalid line ranges. After upgrading from an older index, run `qi index <collection>` to repair them. If the collection is no longer in the config, index its directory with `qi index /path/to/directory`.

## Contributing

Bug reports and pull requests are welcome on [GitHub](https://github.com/itsmostafa/qi/issues). Run `task check` (`go vet` plus `go test ./...`) before opening a pull request.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
