-- Removing a chunk from chunks_fts used to write a delete marker: the
-- postings it cancels, and the text in them, stayed in the index until an
-- optimize happened to rewrite their segment. secure-delete (SQLite 3.42+)
-- removes a deleted row's entries from the segment b-trees in place, so text
-- dropped by a file deletion or an edit leaves the FTS index as soon as the
-- index run commits. The setting persists in chunks_fts_config.
--
-- Optimize first, once, to purge the delete markers and superseded postings
-- that already accumulated; secure-delete only affects later deletions.
INSERT INTO chunks_fts(chunks_fts) VALUES('optimize');
INSERT INTO chunks_fts(chunks_fts, rank) VALUES('secure-delete', 1);

INSERT OR IGNORE INTO schema_version(version) VALUES (8);
