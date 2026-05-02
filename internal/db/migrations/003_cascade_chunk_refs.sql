-- Add ON DELETE CASCADE to chunk_vectors and embeddings so reindexing a
-- changed document (which deletes its chunks) does not fail or orphan rows.
-- SQLite requires a table rebuild to change foreign key actions.

PRAGMA foreign_keys=OFF;

CREATE TABLE chunk_vectors_new (
    chunk_id    INTEGER PRIMARY KEY REFERENCES chunks(id) ON DELETE CASCADE,
    vector      BLOB NOT NULL
);
INSERT INTO chunk_vectors_new(chunk_id, vector)
    SELECT chunk_id, vector FROM chunk_vectors;
DROP TABLE chunk_vectors;
ALTER TABLE chunk_vectors_new RENAME TO chunk_vectors;

CREATE TABLE embeddings_new (
    chunk_id    INTEGER PRIMARY KEY REFERENCES chunks(id) ON DELETE CASCADE,
    provider    TEXT NOT NULL,
    model       TEXT NOT NULL,
    dimension   INTEGER NOT NULL,
    embedded_at TEXT NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO embeddings_new(chunk_id, provider, model, dimension, embedded_at)
    SELECT chunk_id, provider, model, dimension, embedded_at FROM embeddings;
DROP TABLE embeddings;
ALTER TABLE embeddings_new RENAME TO embeddings;

PRAGMA foreign_keys=ON;

INSERT OR IGNORE INTO schema_version(version) VALUES (3);
