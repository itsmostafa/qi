# Configuration

qi is configured with a single YAML file. A fully annotated example is at [`docs/config.example.yaml`](config.example.yaml).

## File location

| Condition | Path |
|-----------|------|
| Default | `~/.config/qi/config.yaml` |
| `$XDG_CONFIG_HOME` set | `$XDG_CONFIG_HOME/qi/config.yaml` |
| Custom | Pass `--config <path>` to any command |

`qi init` creates the file with sensible defaults on first run.

---

## `database_path`

Path to the SQLite database. Supports `~` expansion and paths relative to the config file.

```yaml
database_path: ~/.local/share/qi/qi.db
```

Default: `~/.local/share/qi/qi.db` (or `$XDG_DATA_HOME/qi/qi.db`).

---

## `collections`

A list of document directories to index.

```yaml
collections:
  - name: notes
    path: ~/notes
    description: Personal notes       # optional
    extensions: [.md, .txt]           # optional — omit to use text defaults (.md, .markdown, .txt, .text)
    ignore: [.git, node_modules]      # optional
```

| Field | Required | Description |
|-------|----------|-------------|
| `name` | no | Generated identifier used in search output and CLI flags; filled from `path` if omitted |
| `path` | yes | Directory to index; supports `~` and relative paths |
| `description` | no | Human-readable label |
| `extensions` | no | File extensions to index; omit to use built-in defaults (`.md .markdown .txt .text`) |
| `ignore` | no | Directory/file names to skip during indexing |

Collection names are derived from paths, for example `/Users/alice/Projects/tools/qi` becomes `Projects-tools-qi`. Duplicate generated names or duplicate canonical paths are rejected at startup.

---

## `providers`

All providers are optional. Omitting them degrades gracefully: BM25 search always works without any provider configured.

**Environment variables**: `${VAR}` references in any provider's `base_url` and `api_key` are expanded from the environment when the config loads, so you can keep secrets out of the file:

```yaml
providers:
  embedding:
    name: openai
    base_url: ${QI_EMBED_URL}
    api_key: ${OPENAI_API_KEY}
```

`base_url` is also normalized — a trailing `/v1` is handled automatically, so both `https://api.openai.com` and `https://api.openai.com/v1` work without producing a doubled `/v1/v1` path.

### `providers.embedding`

Enables vector search and hybrid mode (`--mode hybrid`). Must expose an OpenAI-compatible `POST /v1/embeddings` endpoint.

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
| `name` | Identifier (see recipes below) |
| `base_url` | Base URL of the embedding server |
| `model` | Model name passed in the API request |
| `dimension` | Output vector dimension — must match the model |
| `batch_size` | Texts per HTTP request; reduce if the server has payload limits |
| `max_input_chars` | Safety net: truncate any text longer than this many characters before embedding. `0` (default) disables truncation. Set it for models with a hard token limit — e.g. `24000` (~6k tokens) for an 8k-token model |
| `api_key` | Bearer token; set for services that require authentication |

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

When `name: openai`, qi sets `base_url` to `https://api.openai.com` and reads `api_key` from the `OPENAI_API_KEY` environment variable. You can override either field explicitly.

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

### `providers.rerank`

Enables deep search mode (`--mode deep`). Must expose `POST /v1/rerank`.

```yaml
providers:
  rerank:
    name: local-rerank
    base_url: http://localhost:8080
    model: bge-reranker-v2-m3
```

| Field | Description |
|-------|-------------|
| `name` | Identifier |
| `base_url` | Base URL of the rerank server |
| `model` | Model name passed in the request |

Compatible servers include [infinity](https://github.com/michaelfeil/infinity) and similar reranking APIs.

---

## `search`

Controls search behaviour and indexing parameters.

```yaml
search:
  default_mode: hybrid   # lexical | hybrid | deep
  bm25_top_k: 50
  vector_top_k: 50
  rerank_top_k: 10
  rrf_k: 60
  chunk_size: 512
  chunk_overlap: 64
  prefer_extensions: [.md, .txt]   # optional — boost results with these extensions
  extension_boost: 2.0             # optional — multiplier for preferred extensions
```

| Key | Default | Description |
|-----|---------|-------------|
| `default_mode` | `hybrid` | Search mode used when `--mode` is not passed. `lexical` = BM25 only; `hybrid` = BM25 + vector with RRF fusion; `deep` = hybrid then rerank |
| `bm25_top_k` | `50` | Candidate count retrieved from BM25 before fusion |
| `vector_top_k` | `50` | Candidate count retrieved from vector KNN before fusion |
| `rerank_top_k` | `10` | Top N candidates passed to the reranker in `deep` mode |
| `rrf_k` | `60` | Reciprocal Rank Fusion constant; higher values reduce the influence of rank position |
| `chunk_size` | `512` | Target chunk size in characters during indexing |
| `chunk_overlap` | `64` | Reserved; not used by the current break-point chunker |
| `prefer_extensions` | _(none)_ | File extensions whose result scores are multiplied by `extension_boost`, then re-sorted. Empty disables boosting |
| `extension_boost` | `2.0` | Score multiplier applied to results matching `prefer_extensions`. Only used when `prefer_extensions` is set; values `≤ 0` fall back to `2.0` |

`default_mode: hybrid` requires an embedding provider. `default_mode: deep` additionally requires a rerank provider. If the required provider is absent, qi falls back to `lexical` with a warning.

---

## Full example

See [`config.example.yaml`](config.example.yaml) for a complete, annotated configuration file.
