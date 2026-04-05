# Auto-Named Collections Design

**Date:** 2026-04-05
**Status:** Approved

## Problem

Running `qi index` or `qi index /some/path` without `--name` creates an in-memory collection whose name is the full absolute path. This collection is never saved to config, so it cannot be listed with `qi list` or deleted with `qi delete`. Re-indexing the same directory twice results in duplicate chunks and vectors in the database.

## Goal

Every directory indexed by qi should have a stable, human-readable collection name saved to config — automatically, without requiring `--name`. This makes all indexed data manageable via `qi list` and `qi delete`.

## Approach

**Approach B:** Add a `SlugFromPath` helper in the `config` package. Keep the `cmd/index.go` command thin — it does the path lookup and calls `config.AddCollection` when needed.

## Design

### 1. Slug Generation — `config.SlugFromPath`

New exported function in `internal/config/config.go`:

```go
func SlugFromPath(absPath string) string
```

**Algorithm:**
1. Attempt to strip the user home directory prefix from `absPath`. If `os.UserHomeDir()` fails, the path is not under home, or stripping leaves an empty string (path equals home), use the full path.
2. Trim any leading `/`.
3. Replace all `/` with `-`.

**Examples:**
- `/Users/hackbook/Projects/tools/qi` → `Projects-tools-qi`
- `/Users/hackbook/notes` → `notes`
- `/tmp/scratch` → `tmp-scratch`

This function is pure (no I/O beyond the home dir lookup) and lives alongside `ExpandHome` and other path utilities in the config package.

### 2. Path Lookup — `findCollectionByPath`

New unexported helper in `cmd/index.go`:

```go
func findCollectionByPath(collections []config.Collection, absPath string) *config.Collection
```

Scans the slice linearly and returns a pointer to the first collection whose `Path` equals `absPath`, or `nil` if none matches. Paths in config are already resolved to absolute paths by the time they reach the command (via `config.Load`).

### 3. Index Command Behaviour Changes

The `--name` flow and collection-name-arg flow are unchanged.

**No-args case** (`qi index`):
1. Resolve CWD → `absPath`
2. `findCollectionByPath(a.Config.Collections, absPath)`
3. If found: use existing collection (no config write)
4. If not found: `slug = config.SlugFromPath(absPath)`, call `config.AddCollection`, print `Saved collection "<slug>" → <absPath>`, index

**Path-arg case** (`qi index /some/path` or `qi index ~/notes`):
1. Resolve arg → `absPath`
2. Same lookup → same auto-save logic

**Result:** The same directory is never indexed under two different collection names. Chunks and vectors are not duplicated.

### 4. Documentation Updates

- **`README.md`** — Quickstart section updated to show that `qi index` and `qi index <path>` automatically create a named collection. `--name` presented as optional override for custom names.
- **`docs/named-collections.md`** — Updated to explain auto-naming, show example slug output, clarify `--name` is for custom names only.
- **`skills/qi-cli/SKILL.md`** — Usage examples updated to reflect that `--name` is not required for manageable collections.

## Data Model Impact

No schema changes. The `collections` table and `documents.collection` field already support named collections. The only change is that ad-hoc indexing now always produces a config-registered name.

## Testing

- Unit tests for `config.SlugFromPath` covering: path under home, path not under home, path equal to home, trailing slash, nested paths.
- Integration test (existing pattern: real in-memory SQLite) for the no-args index path: verify collection is written to config on first run, not re-written on second run, and that a manually-named collection for the same path is reused rather than duplicated.

## Out of Scope

- Renaming existing auto-named collections when the home directory changes.
- Collision handling if two different paths produce the same slug (extremely rare; not worth complicating the design).
- Migrating existing ad-hoc (unsaved) index data to the new named format.
