-- Record each chunk's inclusive 1-based line range in the raw source.
-- NULL is intentional for chunks created before this migration: line
-- precision is unknown until the owning document is indexed again.
ALTER TABLE chunks ADD COLUMN start_line INTEGER;
ALTER TABLE chunks ADD COLUMN end_line INTEGER;

INSERT OR IGNORE INTO schema_version(version) VALUES (7);
