-- Add ON DELETE CASCADE to chunk_vectors and embeddings so reindexing a
-- changed document (which deletes its chunks) does not fail or orphan rows.
-- SQLite requires a table rebuild to change foreign key actions.

PRAGMA foreign_keys=OFF;

DROP TABLE IF EXISTS chunk_vectors_new;
CREATE TABLE chunk_vectors_new (
    chunk_id    INTEGER PRIMARY KEY REFERENCES chunks(id) ON DELETE CASCADE,
    vector      BLOB NOT NULL
);
INSERT INTO chunk_vectors_new(chunk_id, vector)
    SELECT chunk_id, vector FROM chunk_vectors;
DROP TABLE chunk_vectors;
ALTER TABLE chunk_vectors_new RENAME TO chunk_vectors;

DROP TABLE IF EXISTS embeddings_new;
CREATE TABLE embeddings_new (
    chunk_id    INTEGER PRIMARY KEY REFERENCES chunks(id) ON DELETE CASCADE,
    provider    TEXT NOT NULL,
    model       TEXT NOT NULL,
    dimension   INTEGER NOT NULL DEFAULT 0,
    embedded_at TEXT NOT NULL DEFAULT (datetime('now'))
);
-- Derive dimension from vector length (float32 = 4 bytes each) so that both
-- old schemas (no dimension column) and new ones are handled correctly.
INSERT INTO embeddings_new(chunk_id, provider, model, dimension, embedded_at)
    SELECT e.chunk_id, e.provider, e.model,
           COALESCE(length(cv.vector)/4, 0),
           e.embedded_at
    FROM embeddings e
    LEFT JOIN chunk_vectors cv ON cv.chunk_id = e.chunk_id;
DROP TABLE embeddings;
ALTER TABLE embeddings_new RENAME TO embeddings;

PRAGMA foreign_keys=ON;

INSERT OR IGNORE INTO schema_version(version) VALUES (3);
