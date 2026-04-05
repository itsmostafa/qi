-- qi database schema v1

CREATE TABLE IF NOT EXISTS schema_version (
    version     INTEGER PRIMARY KEY,
    applied_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Collections registered in config
CREATE TABLE IF NOT EXISTS collections (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL UNIQUE,
    path        TEXT NOT NULL,
    description TEXT,
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Content-addressable storage: keyed by SHA-256 hash
CREATE TABLE IF NOT EXISTS content (
    hash        TEXT PRIMARY KEY,  -- hex SHA-256
    body        BLOB NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Documents: one row per file per collection
CREATE TABLE IF NOT EXISTS documents (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    collection   TEXT NOT NULL,
    path         TEXT NOT NULL,          -- relative path within collection
    title        TEXT,
    content_hash TEXT NOT NULL REFERENCES content(hash),
    active       INTEGER NOT NULL DEFAULT 1,  -- 0 = deleted/deactivated
    indexed_at   TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at   TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(collection, path)
);

CREATE INDEX IF NOT EXISTS idx_documents_collection ON documents(collection);
CREATE INDEX IF NOT EXISTS idx_documents_active ON documents(active);
CREATE INDEX IF NOT EXISTS idx_documents_hash ON documents(content_hash);

-- Chunks: sections of a document
CREATE TABLE IF NOT EXISTS chunks (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    content_hash   TEXT NOT NULL REFERENCES content(hash),
    doc_id         INTEGER NOT NULL REFERENCES documents(id),
    seq            INTEGER NOT NULL,          -- order within document
    text           TEXT NOT NULL,
    heading_path   TEXT,                      -- e.g. "Introduction > Background"
    ordinal        INTEGER,                   -- byte offset in document
    content_length INTEGER NOT NULL,
    metadata       TEXT                       -- JSON blob
);

CREATE INDEX IF NOT EXISTS idx_chunks_doc_id ON chunks(doc_id);
CREATE INDEX IF NOT EXISTS idx_chunks_content_hash ON chunks(content_hash);

-- FTS5 full-text search over chunks
CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(
    text,
    heading_path,
    content='chunks',
    content_rowid='id',
    tokenize='porter unicode61'
);

-- FTS triggers to keep chunks_fts in sync
CREATE TRIGGER IF NOT EXISTS chunks_fts_insert AFTER INSERT ON chunks BEGIN
    INSERT INTO chunks_fts(rowid, text, heading_path)
    VALUES (new.id, new.text, new.heading_path);
END;

CREATE TRIGGER IF NOT EXISTS chunks_fts_update AFTER UPDATE ON chunks BEGIN
    INSERT INTO chunks_fts(chunks_fts, rowid, text, heading_path)
    VALUES ('delete', old.id, old.text, old.heading_path);
    INSERT INTO chunks_fts(rowid, text, heading_path)
    VALUES (new.id, new.text, new.heading_path);
END;

CREATE TRIGGER IF NOT EXISTS chunks_fts_delete AFTER DELETE ON chunks BEGIN
    INSERT INTO chunks_fts(chunks_fts, rowid, text, heading_path)
    VALUES ('delete', old.id, old.text, old.heading_path);
END;

-- Vector embeddings stored as raw BLOB (little-endian float32 array)
CREATE TABLE IF NOT EXISTS chunk_vectors (
    chunk_id    INTEGER PRIMARY KEY REFERENCES chunks(id),
    vector      BLOB NOT NULL
);

-- Tracks embedding metadata per chunk
CREATE TABLE IF NOT EXISTS embeddings (
    chunk_id    INTEGER PRIMARY KEY REFERENCES chunks(id),
    provider    TEXT NOT NULL,
    model       TEXT NOT NULL,
    dimension   INTEGER NOT NULL,
    embedded_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Index run history
CREATE TABLE IF NOT EXISTS index_runs (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    collection    TEXT NOT NULL,
    started_at    TEXT NOT NULL DEFAULT (datetime('now')),
    finished_at   TEXT,
    files_scanned INTEGER,
    files_added   INTEGER,
    files_updated INTEGER,
    files_removed INTEGER,
    error         TEXT
);

-- LLM response cache (SHA-256 of prompt+model → response)
CREATE TABLE IF NOT EXISTS llm_cache (
    key         TEXT PRIMARY KEY,  -- hex SHA-256(model+prompt)
    model       TEXT NOT NULL,
    response    TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

INSERT OR IGNORE INTO schema_version(version) VALUES (1);
