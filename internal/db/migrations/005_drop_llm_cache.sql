-- The `ask` command was removed; nothing reads or writes the LLM response cache.

DROP TABLE IF EXISTS llm_cache;

INSERT OR IGNORE INTO schema_version(version) VALUES (5);
