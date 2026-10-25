# Leveraging Jev in qi — research findings

**Date:** 2026-09-20
**Question:** How can qi leverage TypeSafe's Jev model to improve search accuracy, and what would actually make it fast over large monorepos and knowledge bases?
**Method:** 5 search angles, 24 sources fetched, 115 claims extracted, 25 verified under 3-vote adversarial review (2/3 refutes kills a claim). 10 confirmed, 15 killed, 8 findings after synthesis. 106 agent calls.

---

## Bottom line

Jev is cheap, well-documented, and trivially reachable from Go — a single `POST` to
`https://api.typesafe.ai/v1/systemone` with a Bearer token, roughly **$0.0016 per query** to
rerank 30 candidates at **~0.2s per call**.

**But not one claim that it improves retrieval accuracy survived verification** — including
TypeSafe's own CLERC rerank benchmark. A Jev reranker is affordable and easy to bolt on, but
nobody can currently promise it beats qi's existing BM25+dense+RRF ordering. It must ship behind
a flag with a local eval set that *measures* the delta rather than assuming it.

**Jev does not make qi faster.** It adds 200–800 ms of network round trip to a query path that
today completes locally in tens of milliseconds. The real scaling ceiling is in qi's own code,
and it is a SQL problem Jev cannot touch.

**The "large monorepo" half of the question is largely unanswered** and is probably not a Jev
problem at all.

---

## Update 2026-09-22: the monorepo half, and where Jev fits now

**The monorepo goal was an indexing and scan problem, and it is now solved without Jev.** The
target is a ~46 GB repository holding ~2 GB of docs, about 2.7M chunks. Measurements came from
a 424k-chunk Go-source benchmark with stub 768-dim embeddings, and a 2.5M-chunk BM25-only copy
of it:

| Path | Before | After | Change |
|---|---|---|---|
| hybrid `qi query` with a lexical match (424k) | 1.26–1.41s | 0.01–0.06s | vector leg scores only BM25's candidate documents |
| BM25 `err`, 412k matches (2.5M) | 5.2s | 1.2s | snippets computed for selected chunks only |
| BM25 `the` (2.5M) | 11.9s | 1.9s | same |
| no-op `qi index` (2.5M) | 9.2s | 2.7s | unchanged size+mtime skips the read (`file_stat`) |
| no-op `qi index` with embeddings (424k) | 2.1s | 0.9s | same, plus pending-embedding checks in SQL |
| one file over 10 MiB | every run failed | skipped | oversize is a skip, not an error |
| two overlapping hook runs | second failed "database is locked" | second waits | flock on `<db>.index-lock` |

`ext/vec1` (IVF) was evaluated and rejected; CLAUDE.md's vec1 note has the numbers. The vector
leg now re-ranks lexical candidates, so a document sharing no term with the query cannot surface
through it. When BM25 matches nothing, the full scan still runs: 1.33s at 424k, roughly 8–12s at
2.7M. That is the one remaining slow path.

**Jev still does not speed anything up.** A query now takes about 0.04s, so a 0.2–0.8s Jev round
trip would be a 5–20× slowdown instead of the 1.2× it was when the vector scan dominated. Rank the
candidates as follows.

1. **Opt-in reranker over the fused pool** (recommendation 3). This is unchanged, but with a
   stronger case for a flag (`--rerank`) rather than a default. It is still gated on the eval set
   (recommendation 2). Hook it in after `ReciprocalRankFusion` in `internal/search/hybrid.go`,
   before `search.Finalize`. Use one `noul` per (query, chunk) pair, K ≤ 30, with a 1s client
   deadline; on any failure return the fused order.
2. **Precision gate on the relaxation path**. When BM25 falls back to `OR` matching
   (`sanitizeFTSQueryAny`) and the vector leg is bounded by those loose candidates, a Noul such as
   "does this passage address the query?" can drop weak matches rather than show them. It fires
   only on relaxed queries, so its latency is paid rarely. This is the best first Jev feature for
   qi, because the new bounded vector leg makes relaxed candidates matter more.
3. **Semantic-only recall is not a Jev job.** Jev judges candidates it is given; it cannot find
   the document with no lexical overlap that the bounded vector leg now misses. If that gap
   matters, the fix is an ANN index (vec1 once it gains threads or innocuous-vtab status), not a
   judgment model.
4. **Index-time judgments** (recommendation 4) are feasible only as increments. A full pass at
   2.7M chunks is about 12h at 0.2s per call over 12 workers, plus roughly $20 of input tokens.
   Per-document questions batched into one call, run only on files the stat skip reports as
   changed, cost what a pull changes. Use them for doc-type or audience tags that `--path` cannot
   express. Build this only once a concrete filter needs them.
5. **Directory-scoping `Choice`**: skip. It would have traded a round trip for a skipped scan.
   The scan is now bounded, so the trade no longer pays.

---

## Ranked recommendations

### 1. Fix the vector scan — before touching Jev *(high confidence, code-verified)* — **implemented 2026-09-21**

`internal/search/vector.go:57-77` issues one unbounded `SELECT` joining
`chunk_vectors`/`chunks`/`documents`/`embeddings` across the whole active corpus, with no `LIMIT`,
and materializes **every row including `c.text`** — the full chunk body — into Go. It then
deserializes each blob, computes cosine distance, sorts, and only then dedupes to one chunk per
document.

Cost scales with **total corpus bytes**, not just vector count. Pulling `c.text` for every chunk
on every query is pure waste: the snippet is needed for at most `topK` rows.

The first fix is a SQL fix — stop selecting chunk text during the scan, fetch snippets only for
the selected top-K — before any ANN or quantization work. Same-day, no dependency, orthogonal to
the sqlite-vec/WASM blocker in CLAUDE.md. **Required regardless of whether Jev is ever integrated.**

**Done.** The scan now selects `d.id, c.id, d.path, cv.vector` only — identity, the vector, and
the path the scope glob needs before the per-document collapse — and `hydrate` fetches chunk text
and document metadata for the surviving top-K plus their passages in one query, passing the chunk
IDs as a single JSON array to `json_each`. Both reads run inside one `BeginTx` snapshot, so
ranking and hydration cannot see different corpora.

Measured on a synthetic 4,000-chunk corpus, 20 queries at `topK=10` with 3 passages: **15.6 ms →
10.1 ms** at 14.9 MiB of chunk text, **39.0 ms → 21.3 ms** at 59.5 MiB. The saving grows with
corpus bytes, as predicted. A later review pass fused blob decoding into the distance loop and
hoisted the query norm out of it, worth a further **32.4 ms → 29.7 ms (~8%)** at 3,000 chunks ×
768 dimensions — well short of the 3× that pass projected, because `ValidateEmbeddingBlob` still
walks each vector and `database/sql` still boxes every row. Those two are the next lever. Output
is byte-identical to the pre-change binary across hybrid queries with collection, `--path`,
`--since` and `--sort date` filters on a real corpus.

The scan roughly halves, but it does **not** become independent of corpus bytes: at a fixed 4,000
vectors, quadrupling chunk text still takes the new path from 10.1 ms to 21.3 ms. Not selecting a
column is not the same as not reading its row. See open question 2 for the untested hypothesis.

### 2. Build a local eval set *(this is the actual blocker)*

No surviving evidence says *any* reranker improves on qi's current fused ordering. This can only
be answered locally: a small labeled query set over a real qi corpus, measuring nDCG@10 and top-1
for fused-only vs. Jev-reranked. **Until that exists, a Jev reranker is a bet, not an improvement.**

### 3. If Jev at all: the reranker, with the precision gate folded in

Leverage point **(b)** with **(c)** in the same call. K = 20–30, concurrent per-candidate `noul`,
behind a config flag and a ~1s deadline.

The precision gate (c) is mechanically identical to the reranker and strictly cheaper, since it
fires only on the relaxation path — it belongs in the same code path behind a threshold, not as a
separate feature.

### 4. Index-time judgments — the one place the batching economics are real

Leverage point **(d)**: chunk-boundary judgments, frontmatter extraction, document tagging. Many
questions about *one* document in one call is exactly the shape the vendor's 12.2×-cheaper figure
describes, and the cost amortizes over indexing rather than being paid per query.

### 5. SKIP query routing *(leverage point a)*

The one idea whose stated premise — that Jev makes qi faster — is contradicted by the numbers.
Routing would add a ~0.21s minimum round trip to **every** query in order to possibly skip a local
KNN scan that currently costs far less than that. It pays off only once the scan exceeds the RTT,
at which point the right fix is the scan itself (recommendation 1). It also cannot decide anything
before retrieval without sending the raw query off-box, which cuts against local-first.

The hardcoded `3.0` BM25 ratio shortcut at `internal/search/hybrid.go:50` is still worth
revisiting — but with a *local* signal. The proposed zero-network IDF-weighted-fusion alternative
was refuted 0-3, so no specific replacement is currently evidenced.

---

## Verified facts about Jev

### Integration shape *(high confidence, 3-0 + 3-0)*

A thin REST adapter, not an SDK dependency:

- `POST https://api.typesafe.ai/v1/systemone`, `Authorization: Bearer <key>`, body `{state, model, questions}`
- `GET /v1/models` lists model names
- No official Go SDK (Python `typesafe_sdk`, JavaScript `@typesafe-ai/sdk` only). ~10 unofficial
  community Go clients exist but are unvetted and unnecessary for a single JSON POST.
- Needs a **new interface** in qi rather than an implementation of `EmbeddingProvider` — Jev is a
  judgment model, not an embedder — but reuses the same machinery: `net/http`, config-driven base
  URL, client timeout, capped response body, existing `ErrTransient` sentinel. qi's
  `apiBase(baseURL, path)` helper composes the Jev path cleanly.

A live probe with an invalid key returned HTTP 401 `{"detail":{"error_type":"authentication_error"}}`,
proving the endpoint is live and gates on the Bearer header (probed 2026-09-20).

> **Caveat:** Jev is in waitlisted early access. That gates obtaining a key, not the API surface.

### Call shape for reranking *(medium confidence, 2-1 + 3-0 + 3-0)*

Use the vendor cookbook's shape — **one `noul` per (query, candidate) pair, fired concurrently** —
not K candidates packed into one state.

A request carries exactly one `state` plus a map of caller-named questions, with no per-question
state field, so batching K candidates means packing all K into a single state object. The vendor's
rerank cookbook does not do this: it issues 1,200 independent calls (40 queries × 30 candidates)
through a 12-worker thread pool. Its diagram is labeled *"one request per candidate · no request
sees another."*

Three reasons to prefer fan-out:

1. The claim that batched questions cannot contaminate each other was **refuted 0-3** — cross-candidate bias in a packed state is unestablished.
2. The model-jaggedness page states accuracy falls as state grows with content unrelated to the decision, and that Jev suffers context rot. In a packed state, every candidate is irrelevant detail for every other candidate's question.
3. Go gives concurrency for free, which is how the cookbook recovers the latency anyway.

Only a community project (`github.com/hev/jev-rerank`) packs candidates, capping at ~30 per call.

### Structural limits *(high confidence, 3-0 + 3-0)*

| Limit | Value |
|---|---|
| Options per `Choice` | 255 max |
| Levels per `Score` | 2–10 |
| Questions per call | **no documented cap** |
| Token budget | 64k for state + all questions combined |
| | 32k for state + the single longest question |
| Current model | `jev-1.13.0` (`jev-latest`, `jev-preview` both alias it) |

The binding constraint on batch size is the token budget, not a question count — and in practice
context rot bites before 64k does. (`docs.typesafe.ai/limits.md` is a 404; a secondary blog claims
two `Choice` options are reserved (effective 253), which appears nowhere in primary docs — do not
propagate it.)

### Cost and latency *(high confidence, 3-0 + 3-0)*

**Cost is negligible; latency is the real constraint.**

- $42/Btok ($0.042/Mtok) input. **Output tokens free.** No per-call flat fee. Corroborated independently by OpenRouter and Requesty.
- Derived from the rerank cookbook's own totals (1,536,002 input tokens over 1,200 calls ≈ 1,280 tokens and $0.000054 per candidate): **a K=30 rerank costs ~$0.0016 per query.**
- Latency **is** published: ~0.21s implied per single call, 0.27s for a 13-question batch over a ~54K-character state. A third-party measurement reports **206 ms median / 418 ms p95 / 816 ms p99.**

Practical consequence: a rerank adds ~0.2–0.8s to a query path that currently completes locally in
tens of milliseconds. **Latency, not cost, governs whether to enable it.**

### The 12.2×/10× batching headline does not transfer *(high confidence, 3-0 + 3-0)*

It is an input-token amortization effect — paying for one large shared document once instead of N
times. It applies only when many questions share **one** document, never to K distinct candidates
whose text must enter the state regardless. The cookbook says so directly: *"The document dominates
every request. N single-question calls pay for it N times, in N round trips; the batched call pays
once."*

The 10× speed figure is further deflated by the vendor's own note that the baseline sums 13 *serial*
round trips; a concurrent Go client sees no such gap. **The usable planning number for qi is
~0.21–0.27s per call.**

Corollary: the batching economics *do* apply cleanly to index-time uses (recommendation 4) — many
judgments about one chunk in one call. That is the one place the multiplier is real.

### Graceful degradation *(medium confidence, 2-1)*

Cheap to build, but the failure set is wider than one status code: **429** Too Many Requests, **529**
Overloaded, plus network timeout and unconfigured/offline.

Published limits are 250,000 tokens/sec and 1,200 requests/min, explicitly declared volatile by the
vendor on an undated page. `retry-after` is conditional — docs say SDKs honor it *"when the response
carries one"* — so a Go adapter must implement its own exponential backoff and treat the header as a
hint. No server-side timeout or partial-result semantics are documented, so **the client must own its
deadline.**

Rate limits are a non-issue at qi's single-user CLI scale. The binding constraints are per-call
latency and offline fallback.

Design cost is near zero because the pattern already exists: `internal/search/hybrid.go:47-86`
already degrades from hybrid to BM25-only when `h.embedding` is nil, when `Embed` errors, when it
returns no vectors, or when vector search fails — each with an `slog.Warn`. A Jev rerank slots in as
the same shape: on any error, deadline (~1s) or missing config, return the fused order unchanged.

---

## What was refuted — read this before citing any number

**Every claim asserting an accuracy improvement was refuted in verification.**

| Claim | Vote | Source |
|---|---|---|
| Jev CLERC rerank: top-1 5%→18%, top-10 38%→62% | 1-2 | rerank cookbook |
| Batched questions cannot contaminate each other | 0-3 | parallel_questions |
| Cross-encoders *degrade* NDCG 0.3–3.1% after RRF fusion | 0-3 | arXiv 2604.15484 |
| RRF+hybrid is already at its ceiling; improve embeddings instead | 0-3 | arXiv 2604.15484 |
| Per-query IDF-weighted fusion: up to +21.4% NDCG@10, zero regressions | 0-3 | arXiv 2604.15484 |
| sqlite-vec hybrid at 20.9 ms median / 50K chunks | 0-3 | arXiv 2604.15484 |
| Elastic Rerank top-30: ~40% nDCG@10 gain | 0-3 | Elastic search-labs |
| Rerank depth: 90% of gain at ⅓ optimal window | 0-3 | Elastic search-labs |
| Cross-encoder GPU latency table (MiniLM 0.024s … gemma 0.252s) | 0-3 | Elastic search-labs |
| LLM-confidence reranking: honest average ~3%, ~1% over cross-encoders | 1-2 | arXiv 2602.13571 |
| LLM-confidence on plain BM25: 3.0% BEIR / 1.9% TREC | 0-3 | arXiv 2602.13571 |
| The API publishes no latency/limits/pricing | 0-3 | api.md |

**Treat none of those numbers as usable in either direction.** The arXiv identifiers in particular
should be independently checked before anyone cites them — verification flagged several as
misread or misattributed.

What survived is **exclusively Jev's API mechanics, pricing, limits and call shape** — vendor
primary documentation, the correct source tier for those facts, but which establishes nothing
about retrieval quality.

---

## Caveats

- **Time sensitivity.** All docs fetched 2026-09-20. `models.md` carries no publication date and warns rate limits can change without notice. The cookbook pins `jev-1.12` while `models.md` lists `jev-1.13.0`. Price is a point-in-time published rate.
- **Vendor self-benchmarks.** All latency and cost figures are unaudited, on a single ~54K-character state, except the 206/418/816 ms percentiles, which are third-party and secondary.
- **Early access.** Obtaining a key may block implementation regardless of the API being live.
- **The monorepo half is effectively unanswered.** No surviving evidence on ANN/HNSW for CGo-free Go, FTS5 tuning, quantized or binary embeddings with rescoring, sharding by collection, or BM25+dense quality on source code specifically.

---

## Open questions

1. **Does *any* reranker improve on qi's BM25+dense+RRF baseline?** No evidence survived in either direction. Answerable only locally — see recommendation 2.
2. *(Sidestepped 2026-09-22: hybrid search no longer scans the whole corpus; see the update above.)* **What is the actual scaling curve of qi's vector path?** The `c.text` materialization is gone (recommendation 1, implemented), yet the scan still grows with chunk text at a fixed vector count. Untested hypothesis: `chunks.text` is column 5 while `start_line`/`end_line` were appended by migration 007, so the scan's line-range predicate has to read past the large column — through the overflow chain on spilled rows — to reach them. `EXPLAIN QUERY PLAN` on a copy of the local database confirms the shape: with `CREATE INDEX idx_chunks_scan ON chunks(doc_id, start_line, end_line)` the lean scan reports `SEARCH c USING COVERING INDEX` and never reads the table, while the *old* SELECT including `c.text` reports a plain `SEARCH c USING INDEX` and the table read returns. The index is therefore inert until chunk text leaves the scan — this change is its prerequisite, not its alternative. `id` is the rowid and is implicit, so it does not belong in the index. The covering plan also flips `d` to `SCAN d`, so measure on a real corpus before writing migration `008`. Is a pure-Go HNSW/ANN library usable without CGo? Would int8 or binary quantization with full-precision rescoring remove the need for ANN entirely at qi's corpus sizes?
3. *(Answered 2026-09-22: indexing and scan cost, fixed in qi without Jev; qi now also parses `.rst`, `.adoc` and `.mdx`.)* **Is the monorepo goal a retrieval problem or an indexing problem?** qi indexes only Markdown and plaintext, with no AST or symbol awareness. Would tree-sitter/ctags symbol extraction plus gitignore-aware incremental reindexing deliver more than any reranker could?
4. *(Done 2026-09-22.)* **Should `bm25.go` pass its IN-clause IDs through `json_each` too?** The vector hydrate now does, which removed its placeholder loop and its bind-variable cap. `internal/search/bm25.go` still builds a placeholder list in `addPassages` with no cap at all. Mechanical to convert; no measured reason to yet.
5. **Does packing K candidates into one Jev state degrade ranking vs. per-candidate fan-out?** The jaggedness page warns about context rot; the no-contamination claim was refuted; only a community project does the packing. A cheap A/B on a fixed shortlist would settle it and decide the adapter's shape.

---

## Sources

**Primary — vendor:**
`docs.typesafe.ai/api.md` · `/models.md` · `/cookbooks/rerank_typesafe.md` ·
`/cookbooks/parallel_questions.md` · `/model-jaggedness/jev-1.13` · `/llms-full.txt` ·
`typesafe.ai/blog/introducing-system-one-models-and-jev`

**Primary — academic/industry (claims from these were largely refuted; see table above):**
arXiv 2604.15484 · arXiv 2602.13571 · arXiv 2510.20609 · arXiv 2407.02883 · arXiv 2605.25092 ·
arXiv 2009.10791 · arXiv 2604.03455 · arXiv 2411.11767 · arXiv 2604.01733 ·
elastic.co/search-labs/blog/elastic-semantic-reranker-part-3 · sqlite.org/fts5.html ·
github.com/coder/hnsw · github.com/sourcegraph/zoekt · sbert.net embedding-quantization ·
alexgarcia.xyz sqlite-vec stable release

**Code (verified in-session):**
`internal/search/vector.go` · `internal/search/hybrid.go` · `internal/providers/types.go` ·
`internal/providers/embedding.go`
