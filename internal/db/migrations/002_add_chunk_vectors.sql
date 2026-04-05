-- Add vector embedding tables (backfill for databases created before v2)

CREATE TABLE IF NOT EXISTS chunk_vectors (
    chunk_id    INTEGER PRIMARY KEY REFERENCES chunks(id),
    vector      BLOB NOT NULL
);

CREATE TABLE IF NOT EXISTS embeddings (
    chunk_id    INTEGER PRIMARY KEY REFERENCES chunks(id),
    provider    TEXT NOT NULL,
    model       TEXT NOT NULL,
    dimension   INTEGER NOT NULL,
    embedded_at TEXT NOT NULL DEFAULT (datetime('now'))
);

INSERT OR IGNORE INTO schema_version(version) VALUES (2);
