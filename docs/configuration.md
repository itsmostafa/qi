# Configuration

qi is configured with a single YAML file. A fully annotated example is at [`docs/config.example.yaml`](config.example.yaml).

## File location

| Condition | Path |
|-----------|------|
| Default | `~/.config/qi/config.yaml` |
| `$XDG_CONFIG_HOME` set | `$XDG_CONFIG_HOME/qi/config.yaml` |
| Custom | Pass `--config <path>` to any command |

`qi index`, `search`, `query`, `get`, `list`, `stats`, `delete` and `hook` need this file and fail if it is missing; `qi update`, `--version` and `--help` work without it, and `qi doctor` reports a missing file as a failed check. `qi init` writes a starter file when none exists (it includes a sample `~/notes` collection; edit or remove it) and creates the database the file points at. `qi index <path>` and `qi hook install <path>` add a new collection to the file themselves.

---

## `database_path`

Path to the SQLite database. A leading `~` is expanded, and a relative path is resolved against the directory that holds the config file.

```yaml
database_path: ~/.local/share/qi/qi.db
```

The file `qi init` writes sets `~/.local/share/qi/qi.db`. When `database_path` is omitted, qi uses `$XDG_DATA_HOME/qi/qi.db`, or `~/.local/share/qi/qi.db` when that variable is unset.

---

## `collections`

A list of collections. A collection is one directory tree that qi indexes and searches under a single name.

```yaml
collections:
  - name: notes
    path: ~/notes
    description: Personal notes       # optional
    extensions: [.md, .mdx, .txt]     # optional — omit to use the defaults (.md, .markdown, .txt, .text)
    ignore: [drafts, "*.draft.md"]    # optional
```

| Field | Required | Description |
|-------|----------|-------------|
| `name` | no | Generated from `path` when the config loads; the value in the file is not used as the name (see below). Search output and `-c` use the generated name |
| `path` | yes | Directory to index; a leading `~` is expanded and a relative path is resolved against the config file's directory |
| `description` | no | Human-readable label |
| `extensions` | no | File extensions to index; omit to use the defaults (`.md .markdown .txt .text`). Setting it replaces the defaults, so `.mdx`, `.rst`, `.adoc` and `.asciidoc` are indexed only when listed. Entries are case-insensitive and the leading dot is optional (`md` and `.MD` both mean `.md`). `.md`, `.markdown` and `.mdx` are parsed as Markdown (in `.mdx`, `import`/`export` lines and JSX tag lines are ignored and text inside JSX blocks is indexed); `.rst` as reStructuredText and `.adoc`/`.asciidoc` as AsciiDoc, with section titles as heading paths; every other listed extension is indexed as plain text. Unchanged files are not reparsed after an upgrade that changes parsing: run `qi index --force <collection>` once, for example after upgrading if you already indexed `.mdx` files as plain text |
| `ignore` | no | Globs of directories and files to skip during indexing |

Each `ignore` entry is a glob (Go `path.Match` syntax: `*`, `?`, `[...]`)
matched against a file's collection-relative path, its basename and every
parent directory. So `drafts` and `drafts/*` both skip everything under a
top-level `drafts` directory, `archive` skips a directory or file named
`archive` at any depth, and `*.draft.md` skips those files at any depth.
Negation (`!`) and `.gitignore` files are not supported. A malformed glob is
rejected when the config loads. Adding a pattern removes the files it now
covers on the next `qi index`, the same way a deleted file is removed. The
`--path` filter on `qi search` and `qi query` uses the same matching, so
`--path 'meetings/*'` limits results to files under `meetings`.

Some directories are always skipped, without an `ignore` entry: every
directory whose name starts with `.` (`.git`, `.obsidian`, ...), and any
directory named `node_modules`, `vendor`, `venv`, `__pycache__`, `target`,
`dist`, `build` or `out`, along with a few other tool caches. The
collection's own root is never skipped, so a collection at `~/.notes` or
`~/build` still indexes. Symlinks inside a collection are not followed.

Files over 10 MiB are skipped, as are binary files (a NUL byte in the first
8000 bytes) whatever their extension.

A collection's name is its directory's own name: `~/Projects/tools/qi`
becomes `qi`. When two collections would share a name, each takes on leading
path segments until the names differ, while other collections keep their short
names. For example, with these three paths:

| Path | Name |
|------|------|
| `~/work/notes` | `work-notes` |
| `~/personal/notes` | `personal-notes` |
| `~/wiki` | `wiki` |

Segments are counted from your home directory, so a path directly under `~`
has only one segment to give and keeps its name: `~/notes` next to
`~/work/notes` gives `notes` and `work-notes`. Two entries that still end up
with the same name, or that point at the same directory, are rejected when the
config loads. See [Collections](named-collections.md) for how renames work.

---

## `providers`

The only provider is `embedding`, and it is optional: BM25 search works without it.

**Environment variables**: `${VAR}` (or `$VAR`) references in `base_url` and `api_key` are expanded from the environment when the config loads, so you can keep secrets out of the file. An unset variable expands to an empty string.

```yaml
providers:
  embedding:
    name: openai
    base_url: ${QI_EMBED_URL}
    api_key: ${OPENAI_API_KEY}
```

`base_url` is also normalized — a trailing `/v1` is handled automatically, so both `https://api.openai.com` and `https://api.openai.com/v1` work without producing a doubled `/v1/v1` path.

### `providers.embedding`

Enables vector search and hybrid mode (`qi query --mode hybrid`). The server must expose an OpenAI-compatible `POST /v1/embeddings` endpoint. With a provider configured, `qi index` embeds every chunk of the collection that lacks a current vector at the end of each run.

```yaml
providers:
  embedding:
    name: ollama
    base_url: http://localhost:11434
    model: nomic-embed-text
    dimension: 768
    batch_size: 32      # optional, default 32
    max_input_chars: 0  # optional, 0 = no limit
    api_key: ""         # optional
```

| Field | Description |
|-------|-------------|
| `name` | Free-form label stored with each vector (see recipes below). Only `openai` changes behaviour |
| `base_url` | Base URL of the embedding server |
| `model` | Model name passed in the API request |
| `dimension` | Output vector dimension; must be positive and match the model |
| `batch_size` | Texts per HTTP request (default `32`); reduce if the server has payload limits |
| `max_input_chars` | Safety net: truncate any text longer than this many characters before embedding. `0` (default) disables truncation. Set it for models with a hard token limit — e.g. `24000` (~6k tokens) for an 8k-token model |
| `api_key` | Bearer token; set for services that require authentication |

The embedding fingerprint is a hash of `name`, `base_url`, `model`, `dimension` and `max_input_chars`, stored with every vector. Changing any of those five fields makes search ignore the existing vectors, and the next `qi index` of each collection re-embeds all of its chunks. Changing `api_key` or `batch_size` does not.

**Recipes**

_Ollama_ (local, free):
```yaml
embedding:
  name: ollama
  base_url: http://localhost:11434
  model: nomic-embed-text
  dimension: 768
```

_llama.cpp server_ (local, free):
```yaml
embedding:
  name: llamacpp
  base_url: http://localhost:8080
  model: nomic-embed-text   # informational only; llama.cpp ignores it
  dimension: 768            # match the model you loaded
```

_LM Studio_ (local, free):
```yaml
embedding:
  name: lmstudio
  base_url: http://localhost:1234
  model: nomic-ai/nomic-embed-text-v1.5-GGUF   # must match model loaded in LM Studio
  dimension: 768                                 # match the model you loaded
```

Load an embedding model in LM Studio (Models → Search → load), then start the local server (Local Server tab). The model field must match the identifier shown in LM Studio exactly.

_OpenAI_ (cloud, requires API key):
```yaml
embedding:
  name: openai
  model: text-embedding-3-small
  dimension: 1536
  # base_url and api_key are filled automatically from OPENAI_API_KEY
```

When `name: openai` and the field is empty, qi sets `base_url` to `https://api.openai.com` and reads `api_key` from the `OPENAI_API_KEY` environment variable. Setting either field explicitly overrides it.

Other OpenAI-compatible services (e.g. Azure, Together, Mistral):
```yaml
embedding:
  name: together
  base_url: https://api.together.xyz
  api_key: sk-...
  model: togethercomputer/m2-bert-80M-8k-retrieval
  dimension: 768
```

---

## `search`

Controls search behaviour and indexing parameters.

```yaml
search:
  default_mode: hybrid   # lexical | hybrid (deep is an alias for hybrid)
  bm25_top_k: 50
  vector_top_k: 50
  rrf_k: 60
  chunk_size: 512
  prefer_extensions: [.md, .txt]   # optional — boost results with these extensions
  extension_boost: 2.0             # optional — multiplier for preferred extensions
```

| Key | Default | Description |
|-----|---------|-------------|
| `default_mode` | `hybrid` | Mode `qi query` uses when `--mode` is not passed. `lexical` = BM25 only; `hybrid` = BM25 + vector with RRF fusion; `deep` = accepted as an alias of `hybrid`. `qi search` always uses BM25 |
| `bm25_top_k` | `50` | Candidate **documents** retrieved from BM25 by `qi search` and `qi query` (a document is represented by its best-matching chunk) |
| `vector_top_k` | `50` | Candidate **documents** retrieved from vector search before fusion; raised to `--limit` when the limit is larger |
| `rrf_k` | `60` | Reciprocal Rank Fusion constant; higher values reduce the influence of rank position |
| `chunk_size` | `512` | Target chunk size in characters during indexing. Must be positive; a value `≤ 0` is rejected at load time |
| `prefer_extensions` | _(none)_ | File extensions whose result scores are multiplied by `extension_boost`, then re-sorted. Applies to `qi query` in hybrid mode with an embedding provider configured; `qi search` and lexical mode ignore it. Empty disables boosting |
| `extension_boost` | `2.0` | Score multiplier applied to results matching `prefer_extensions`. Only used when `prefer_extensions` is set; values `≤ 0` fall back to `2.0` |

Hybrid mode needs an embedding provider; without one, `qi query` prints a warning and runs a lexical search. An unknown `default_mode` is rejected at load time.

---

## Full example

See [`config.example.yaml`](config.example.yaml) for a complete, annotated configuration file.
