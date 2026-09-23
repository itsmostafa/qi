-- Record each document's file size and modification time as observed when it
-- was indexed. A file whose stat still matches is skipped without being read,
-- so a no-op `qi index` over a large tree costs a directory walk, not a read
-- and SHA-256 of every indexed byte. NULL for rows indexed before this
-- migration: the next run reads them once and records the stat.
ALTER TABLE documents ADD COLUMN file_stat TEXT;

INSERT OR IGNORE INTO schema_version(version) VALUES (9);
