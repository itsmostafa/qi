-- Track which embedding identity (model, dimension, truncation behavior)
-- produced each vector, so a model/dimension/truncation change invalidates
-- old vectors instead of silently comparing them against a new model's
-- query embeddings. Plain additive ALTER TABLE — unlike migration 003, this
-- does not need a table rebuild, so it carries no concurrent-migration risk.
--
-- Existing rows get '' (no prior config recorded max_input_chars, so exact
-- backfill isn't possible). '' matches no active fingerprint, so every
-- pre-upgrade chunk is treated as stale and gets re-embedded once on the
-- next `qi index` run.

ALTER TABLE embeddings ADD COLUMN fingerprint TEXT NOT NULL DEFAULT '';

INSERT OR IGNORE INTO schema_version(version) VALUES (4);
