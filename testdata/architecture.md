# Architecture

qi uses a content-addressable storage model inspired by qmd.

## Storage

Documents are stored in SQLite with the following tables:

- `content`: stores raw document bytes keyed by SHA-256 hash
- `documents`: one row per file, references content by hash
- `chunks`: sections of documents used for search and embedding
- `chunks_fts`: FTS5 virtual table for BM25 search

## Chunking

The break-point chunker assigns scores to potential chunk boundaries:

- Headings: score 100
- Code fences: score 80
- Blank lines: score 20

Distance decay reduces the score as the chunk diverges from the target size.

## Search

BM25 search uses SQLite FTS5's built-in ranking function.
Vector search uses sqlite-vec for KNN queries on embeddings.
Hybrid search combines both using Reciprocal Rank Fusion (RRF).
