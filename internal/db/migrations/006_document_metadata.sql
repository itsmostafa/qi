-- Promote YAML frontmatter to document-level fields so recency filters and tag
-- lookups have something to query. Both are NULL for documents indexed before
-- this migration; the next `qi index` of a changed file backfills them.
ALTER TABLE documents ADD COLUMN doc_timestamp TEXT;
ALTER TABLE documents ADD COLUMN tags TEXT;  -- JSON array

CREATE INDEX IF NOT EXISTS idx_documents_timestamp ON documents(doc_timestamp);

-- The collections table was never populated by indexing: `qi index` writes the
-- config file instead, so the table held rows only after a collection rename.
-- `qi list` and `qi stats` therefore disagreed. Config is the single source of
-- truth for which collections exist; the documents table is the source of truth
-- for which are indexed.
DROP TABLE IF EXISTS collections;
