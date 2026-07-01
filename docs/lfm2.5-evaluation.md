# LFM2.5 retrieval + embedding evaluation

**Date:** 2026-06-20
**Question:** Should qi replace its current retrieval/embedding models with LiquidAI's
LFM2.5 pair — `LFM2.5-ColBERT-350M` (late-interaction retrieval) and
`LFM2.5-Embedding-350M` (dense bi-encoder)?

**Verdict: 🔴 NO-GO for both.** No code was changed. Details below.

---

## Phase 0 — Baseline (what qi does today)

qi has **no model baked into the binary** — it is a thin OpenAI-compatible HTTP client.
"The current model" lives entirely in config + the live DB.

| Aspect | Current state |
|---|---|
| **Embedding model (actual)** | `text-embedding-embeddinggemma-300m-qat` — **Google EmbeddingGemma-300M**, served locally via **LM Studio** (`http://localhost:1234`) |
| **Dimension** | **768** (confirmed in DB: 6,827 chunks × 768 float32 = 20,972,544 bytes) |
| **Retrieval** | BM25 (FTS5, always on) **+** dense vector KNN → **RRF fusion** (`internal/search/hybrid.go`) |
| **Vector index** | Pure-Go **brute-force cosine KNN** over float32 BLOBs in `chunk_vectors`; no ANN, no PLAID (`internal/search/vector.go`) |
| **Late interaction / ColBERT** | **None exists.** Storage is one vector per chunk. |
| **Rerank** | Provider code exists (`/v1/rerank`) but is **never wired in** — `NewRerank` is uncalled; `"deep"` mode == `"hybrid"` (`cmd/query.go:47`). Dead config knob. |
| **Chunk size** | `chunk_size: 512` is in **runes** (`internal/chunker/breakpoint.go:20`), i.e. ≈128 tokens — not tokens. |

### Embedding call sites (every one)
1. `internal/providers/embedding.go` — the `/v1/embeddings` HTTP adapter
2. `internal/indexer/embedder.go` — embeds chunks at index time → `chunk_vectors` + `embeddings` metadata
3. `internal/search/hybrid.go:70` — embeds the **query** at search time
4. `internal/app/app.go:53` — wiring
5. `internal/config/{config.go,defaults.go}` — `EmbeddingProviderConfig` + template
6. `internal/db/vec.go` + `internal/search/vector.go` — BLOB (de)serialize + KNN
7. Docs: `README.md`, config template, `qi-cli` skill

### Eval harness
**None exists.** No recall@k / MRR / nDCG; `testdata/` has 4 toy files; tests are unit tests
on in-memory SQLite. Proposed minimal harness (see end): reuse the live DB (6,827 real
chunks), hand-label ~20–30 NL queries → expected chunk, script `qi search --json` for
**Recall@10 + MRR@10 + p50 latency**, `du -h` on `chunk_vectors` for index size.

---

## Phase 1 — Decision gate

### 1. Is it worth it?

The goal assumed we'd replace a weak baseline. **We're not.** The incumbent is
**EmbeddingGemma-300M**, the **#1-ranked open multilingual embedding model under 500M params
on MTEB** (multilingual, English, *and* code leaderboards), 100+ languages, 2K context,
Matryoshka (768→512/256/128), already fully on-device.

| | **EmbeddingGemma-300M (current)** | **LFM2.5-Embedding-350M (proposed)** |
|---|---|---|
| Quality evidence | **#1 MTEB <500M** (published) | NanoBEIR nDCG@10 0.577 / MKQA R@20 0.691. **No MTEB, no head-to-head vs Gemma.** |
| Languages | **100+** | 11 |
| Dimension | 768 (+ Matryoshka shrink) | 1024 **fixed**, no Matryoshka |
| Context | 2K tokens | 512 tokens |
| Index size (6,827 chunks) | ~20 MB | **~28 MB (+33%)** — and +33% per-query brute-force KNN cost |
| On-device | ✅ already running | ✅ (GGUF) |

**Worth it: No.** Swapping a proven best-in-class model for one with no published evidence
of beating it, fewer languages, no Matryoshka, +33% index size/compute, and a mandatory
full re-embed of 6,827 chunks is a **lateral-to-downward move**. LFM2.5's "drop-in RAG
replacement" is a generic claim, not a claim of superiority over EmbeddingGemma. Local-first
is already satisfied by the incumbent.

#### Does late-interaction (ColBERT) fit qi at all? No.

ColBERT is an *addition*, not a swap — qi has zero late-interaction machinery. Adopting
`LFM2.5-ColBERT-350M` would require:

- A new schema storing **per-token** vectors: a 512-token chunk → up to 512×128 ≈ 65k floats
  vs today's 1,024 → roughly **25–64× the vector store**, brute-forced with no ANN/PLAID.
- A MaxSim scoring path in Go (qi only does cosine over single vectors).
- **No native transport.** `llama-server` does **not** serve MaxSim — its benchmark
  "Query embedding + MaxSim" runs client-side; there's no OpenAI-compatible endpoint
  returning per-token ColBERT vectors. PyLate is the only first-class runner → a **Python
  sidecar**, violating local-first / Go-native preference.
- Even the cheapest variant (wire the rerank slot to MaxSim-rerank top-N) still needs that
  sidecar or non-standard per-token retrieval.

All for **+0.028 nDCG** (0.605 vs 0.577) on Liquid's own benchmark. Not justified.

### 2. Native on Mac vs API (for the record, moot given verdict)

If we ever did the embedding swap, the clean path would be **GGUF via
`llama-server --embeddings`** (exposes OpenAI-compatible `/v1/embeddings` — qi already
speaks it, zero Go transport code, no sidecar). The only required code change would be
**asymmetric `query:` / `document:` prefixing** (the model degrades silently without it),
confined to the embedding layer. MLX/PyLate/hosted API are worse fits.

### 3. GO / NO-GO

**🔴 NO-GO — both models.**

- **ColBERT-350M: NO-GO (decisive).** Architectural mismatch — late-interaction does not fit
  qi's single-vector dense store without a 25–64× index, a new MaxSim subsystem, and a Python
  sidecar (no native MaxSim transport). Marginal quality gain.
- **Embedding-350M: NO-GO.** The real incumbent is EmbeddingGemma-300M (#1 MTEB <500M), not a
  weak baseline. LFM2.5-Embedding offers no measured quality advantage, fewer languages, no
  Matryoshka, +33% index size/compute, and forces a full re-embed — a lateral-to-downward
  swap. Local-first already satisfied.

---

## Side-findings (model-agnostic, no action taken)

1. **qi embeds raw text with no instruction prefixes.** Both EmbeddingGemma and LFM2.5 expect
   asymmetric query/document (task) prefixes. The current setup likely leaves retrieval
   quality on the table **today**, independent of any swap. Worth a separate issue.
2. **The rerank provider is dead code** — fully implemented, never wired. Enabling reranking
   in `"deep"` mode (existing, local-compatible `/v1/rerank` path) is a far cheaper,
   lower-risk quality win than swapping the embedder.

## To overturn this NO-GO

Build the minimal eval harness above and A/B EmbeddingGemma vs LFM2.5-Embedding on the real
6,827-chunk corpus (Recall@10 / MRR / latency / index size). Swap only if LFM2.5 measurably
wins on *this* data.

## Sources

- https://huggingface.co/LiquidAI/LFM2.5-Embedding-350M
- https://huggingface.co/LiquidAI/LFM2.5-ColBERT-350M
- https://ai.google.dev/gemma/docs/embeddinggemma/model_card
- https://developers.googleblog.com/en/introducing-embeddinggemma/
- https://huggingface.co/blog/embeddinggemma
