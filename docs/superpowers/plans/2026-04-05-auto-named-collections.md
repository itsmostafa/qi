# Auto-Named Collections Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When `qi index` or `qi index <path>` is run without `--name`, automatically generate a human-readable collection name from the directory path, save it to config on first run, and reuse the existing collection on subsequent runs — eliminating duplicate data.

**Architecture:** A `SlugFromPath` helper in the `config` package converts an absolute path to a slug by stripping the home directory prefix and replacing `/` with `-`. The index command uses a `findCollectionByPath` helper to check whether the path is already in config; if not, it auto-generates the slug and calls `config.AddCollection` before indexing.

**Tech Stack:** Go stdlib (`os`, `strings`, `path/filepath`), existing `config` package, Cobra command pattern already in `cmd/index.go`.

---

## File Map

| File | Change |
|---|---|
| `internal/config/config.go` | Add `SlugFromPath(absPath string) string` |
| `internal/config/config_test.go` | Add `TestSlugFromPath` |
| `cmd/index.go` | Add `findCollectionByPath`; update no-args and path-arg cases |
| `README.md` | Update Quickstart section |
| `docs/named-collections.md` | Update to describe auto-naming |
| `skills/qi-cli/SKILL.md` | Update index examples |
| `CLAUDE.md` | Document auto-naming behavior |

---

### Task 1: `config.SlugFromPath` — tests then implementation

**Files:**
- Modify: `internal/config/config_test.go`
- Modify: `internal/config/config.go`

- [ ] **Step 1: Write the failing tests**

First, add `"strings"` to the import block in `internal/config/config_test.go` (it already imports `"os"` and `"path/filepath"`):

```go
import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)
```

Then append to `internal/config/config_test.go`:

```go
func TestSlugFromPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "nested path under home",
			input:    filepath.Join(home, "Projects", "tools", "qi"),
			expected: "Projects-tools-qi",
		},
		{
			name:     "single dir under home",
			input:    filepath.Join(home, "notes"),
			expected: "notes",
		},
		{
			name:     "path equals home",
			input:    home,
			expected: strings.ReplaceAll(strings.TrimPrefix(home, "/"), "/", "-"),
		},
		{
			name:     "path not under home",
			input:    "/tmp/scratch",
			expected: "tmp-scratch",
		},
		{
			name:     "root-level path not under home",
			input:    "/var/log",
			expected: "var-log",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SlugFromPath(tt.input)
			if got != tt.expected {
				t.Errorf("SlugFromPath(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
go test ./internal/config/... -run TestSlugFromPath -v
```

Expected: `FAIL` — `undefined: SlugFromPath`

- [ ] **Step 3: Implement `SlugFromPath` in `internal/config/config.go`**

Add after the `ExpandHome` function (around line 107):

```go
// SlugFromPath converts an absolute path to a collection name slug.
// It strips the user home directory prefix if present, then replaces
// path separators with hyphens.
//
// Examples:
//   /Users/alice/Projects/tools/qi → Projects-tools-qi
//   /Users/alice/notes             → notes
//   /tmp/scratch                   → tmp-scratch
func SlugFromPath(absPath string) string {
	if home, err := os.UserHomeDir(); err == nil {
		prefix := home + "/"
		if strings.HasPrefix(absPath, prefix) {
			rel := absPath[len(prefix):]
			if rel != "" {
				return strings.ReplaceAll(rel, "/", "-")
			}
		}
	}
	// Path equals home, not under home, or home lookup failed: use full path.
	return strings.ReplaceAll(strings.TrimPrefix(absPath, "/"), "/", "-")
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
go test ./internal/config/... -run TestSlugFromPath -v
```

Expected: all subtests `PASS`

- [ ] **Step 5: Run full check**

```bash
task check
```

Expected: all checks pass.

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add SlugFromPath for auto-naming collections"
```

---

### Task 2: `findCollectionByPath` + updated index command

**Files:**
- Modify: `cmd/index.go`

- [ ] **Step 1: Write a failing test for `findCollectionByPath`**

Create `cmd/index_test.go`:

```go
package cmd

import (
	"testing"

	"github.com/itsmostafa/qi/internal/config"
)

func TestFindCollectionByPath(t *testing.T) {
	cols := []config.Collection{
		{Name: "notes", Path: "/home/user/notes"},
		{Name: "code", Path: "/home/user/projects"},
	}

	t.Run("found", func(t *testing.T) {
		got := findCollectionByPath(cols, "/home/user/notes")
		if got == nil {
			t.Fatal("expected non-nil result")
		}
		if got.Name != "notes" {
			t.Errorf("expected name %q, got %q", "notes", got.Name)
		}
	})

	t.Run("not found", func(t *testing.T) {
		got := findCollectionByPath(cols, "/home/user/other")
		if got != nil {
			t.Errorf("expected nil, got %+v", got)
		}
	})

	t.Run("empty slice", func(t *testing.T) {
		got := findCollectionByPath(nil, "/home/user/notes")
		if got != nil {
			t.Errorf("expected nil on empty slice, got %+v", got)
		}
	})
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test ./cmd/... -run TestFindCollectionByPath -v
```

Expected: `FAIL` — `undefined: findCollectionByPath`

- [ ] **Step 3: Update `cmd/index.go`**

Replace the entire file with:

```go
package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/itsmostafa/qi/internal/app"
	"github.com/itsmostafa/qi/internal/config"
	"github.com/spf13/cobra"
)

var indexName string

var indexCmd = &cobra.Command{
	Use:   "index [path|collection]",
	Short: "Index documents into the knowledge base",
	Long: `Index documents from a directory or named collection.

With no arguments, indexes the current directory (auto-named from path).
With a path argument (absolute, relative, or starting with ~), indexes that directory (auto-named from path).
With a collection name, indexes the named collection from config.
With no arguments and no path-like arg, indexes all configured collections.

A collection name is derived automatically from the directory path on first run:
  /Users/alice/Projects/tools/qi → Projects-tools-qi

Use --name to choose a custom collection name instead:
  qi index ~/notes --name notes
  qi index src --name src`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		a, err := app.New(ctx, cfgFile)
		if err != nil {
			return err
		}
		defer a.Close()

		// --name: treat arg (or cwd) as path, save to config, then index.
		if indexName != "" {
			var dir string
			if len(args) > 0 {
				dir, err = filepath.Abs(config.ExpandHome(args[0]))
			} else {
				dir, err = os.Getwd()
			}
			if err != nil {
				return fmt.Errorf("resolving path: %w", err)
			}
			if _, err := os.Stat(dir); err != nil {
				return fmt.Errorf("path %q does not exist", dir)
			}
			col := config.Collection{Name: indexName, Path: dir}
			cfgPath := cfgFile
			if cfgPath == "" {
				cfgPath = config.DefaultConfigPath()
			}
			if err := config.AddCollection(cfgPath, col); err != nil {
				return fmt.Errorf("saving collection to config: %w", err)
			}
			fmt.Printf("Saved collection %q → %s\n", indexName, dir)
			return runIndex(ctx, a, []config.Collection{col})
		}

		// If arg looks like a path, index it as a (possibly new) named collection.
		if len(args) > 0 && isPathArg(args[0]) {
			dir, err := filepath.Abs(config.ExpandHome(args[0]))
			if err != nil {
				return fmt.Errorf("resolving path: %w", err)
			}
			col, err := autoCollection(a, dir)
			if err != nil {
				return err
			}
			return runIndex(ctx, a, []config.Collection{col})
		}

		// No args: index current directory as a (possibly new) named collection.
		if len(args) == 0 {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting current directory: %w", err)
			}
			col, err := autoCollection(a, cwd)
			if err != nil {
				return err
			}
			return runIndex(ctx, a, []config.Collection{col})
		}

		// Otherwise treat arg as a collection name.
		name := args[0]
		for _, c := range a.Config.Collections {
			if c.Name == name {
				return runIndex(ctx, a, []config.Collection{c})
			}
		}
		return fmt.Errorf("collection %q not found in config", name)
	},
}

func init() {
	indexCmd.Flags().StringVar(&indexName, "name", "", "save directory as a named collection in config")
}

// autoCollection returns an existing collection for absPath if one is already in
// config (matched by path), or generates a slug name, saves it to config, and
// returns the new collection.
func autoCollection(a *app.App, absPath string) (config.Collection, error) {
	if existing := findCollectionByPath(a.Config.Collections, absPath); existing != nil {
		return *existing, nil
	}
	slug := config.SlugFromPath(absPath)
	col := config.Collection{Name: slug, Path: absPath}
	cfgPath := cfgFile
	if cfgPath == "" {
		cfgPath = config.DefaultConfigPath()
	}
	if err := config.AddCollection(cfgPath, col); err != nil {
		return config.Collection{}, fmt.Errorf("saving collection to config: %w", err)
	}
	fmt.Printf("Saved collection %q → %s\n", slug, absPath)
	return col, nil
}

// findCollectionByPath returns a pointer to the first collection whose Path
// equals absPath, or nil if none matches.
func findCollectionByPath(collections []config.Collection, absPath string) *config.Collection {
	for i := range collections {
		if collections[i].Path == absPath {
			return &collections[i]
		}
	}
	return nil
}

// isPathArg returns true if s looks like a filesystem path rather than a collection name.
func isPathArg(s string) bool {
	return strings.HasPrefix(s, "/") ||
		strings.HasPrefix(s, "./") ||
		strings.HasPrefix(s, "../") ||
		strings.HasPrefix(s, "~") ||
		s == "." || s == ".."
}

func runIndex(ctx context.Context, a *app.App, collections []config.Collection) error {
	for _, col := range collections {
		fmt.Printf("Indexing %q (%s)...\n", col.Name, col.Path)
		stats, err := a.Indexer.Index(ctx, col)
		if err != nil {
			fmt.Printf("  error: %v\n", err)
			continue
		}
		fmt.Printf("  scanned=%d added=%d updated=%d removed=%d time=%s\n",
			stats.FilesScanned, stats.FilesAdded, stats.FilesUpdated, stats.FilesRemoved, stats.Duration.Round(1000000))
	}
	return nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
go test ./cmd/... -run TestFindCollectionByPath -v
```

Expected: all subtests `PASS`

- [ ] **Step 5: Run full check**

```bash
task check
```

Expected: all checks pass.

- [ ] **Step 6: Commit**

```bash
git add cmd/index.go cmd/index_test.go
git commit -m "feat(cmd): auto-name collections from directory path on first index"
```

---

### Task 3: Documentation updates

**Files:**
- Modify: `README.md`
- Modify: `docs/named-collections.md`
- Modify: `skills/qi-cli/SKILL.md`
- Modify: `CLAUDE.md`

- [ ] **Step 1: Update `README.md` Quickstart section**

Replace the Quickstart block (lines ~52–68):

```markdown
## Quickstart

```sh
# Initialize config and database
qi init

# Index current directory (auto-named from path)
qi index

# Or index a specific path (also auto-named)
qi index ~/notes

# Use --name to choose a custom collection name
qi index ~/notes --name notes

# Re-index a named collection
qi index notes

# Search
qi search "my query"

# Search a specific collection
qi search "my query" -c notes

# Hybrid search (BM25 + vector, requires embedding provider)
qi query "my query" --mode hybrid

# Hybrid search a specific collection
qi query "my query" --mode hybrid -c notes

# Ask a question (requires generation provider)
qi ask "how does X work?"

# Ask a question to a specific collection
qi ask "how does X work?" -c notes

# List all named collections
qi list

# Delete a named collection and all its indexed data
qi delete notes

# Health check
qi doctor
```
```

- [ ] **Step 2: Update `docs/named-collections.md`**

Replace the entire file with:

```markdown
# Named Collections

Every directory you index gets a named collection automatically. The name is derived from the directory path by stripping your home directory prefix and replacing `/` with `-`:

```sh
qi index ~/Projects/tools/qi
# → Saved collection "Projects-tools-qi" → /Users/alice/Projects/tools/qi
```

On subsequent runs for the same path, qi finds the existing collection and re-indexes without creating a duplicate:

```sh
qi index ~/Projects/tools/qi
# → Indexing "Projects-tools-qi" (/Users/alice/Projects/tools/qi)...
```

Use `--name` to choose a custom collection name instead of the auto-generated one:

```sh
qi index ~/notes --name notes
qi index ~/work/project/docs --name project
```

Re-index a collection by name:

```sh
qi index notes
```

Re-index everything (useful in a cron job or shell alias):

```sh
qi index
```

Collections are stored in `~/.config/qi/config.yaml` and can be edited directly:

```yaml
collections:
  - name: notes
    path: ~/notes
    extensions: [.md, .txt]
  - name: project
    path: ~/work/project/docs
```

Delete a collection and all its indexed data:

```sh
qi delete notes
```
```

- [ ] **Step 3: Update `skills/qi-cli/SKILL.md` index section and workflows**

In the `### qi index` command section, replace:

```markdown
# --name saves the directory as a named collection in config, then indexes it
qi index ~/notes --name notes         # save + index ~/notes as "notes"
qi index --name notes                 # save + index current directory as "notes"
```

with:

```markdown
# directories are auto-named from their path on first run:
# ~/Projects/tools/qi → "Projects-tools-qi"
qi index                              # indexes and auto-names current directory
qi index ~/notes                      # indexes and auto-names ~/notes

# --name overrides the auto-generated name with a custom one
qi index ~/notes --name notes         # save + index ~/notes as "notes"
qi index --name notes                 # save + index current directory as "notes"
```

In the `## Typical workflows` section, replace:

```markdown
**Index and search (no provider needed):**
```bash
qi init
qi index ~/notes --name notes    # saves and indexes ~/notes as "notes"
qi search "my keyword" -c notes
```

**Manage named collections:**
```bash
qi list                          # see all configured collections
qi index ~/projects --name code  # add a new collection
qi delete old-notes              # remove collection data + config entry
```
```

with:

```markdown
**Index and search (no provider needed):**
```bash
qi init
qi index ~/notes                 # auto-named "notes" on first run
qi search "my keyword" -c notes
```

**Manage named collections:**
```bash
qi list                          # see all configured collections
qi index ~/projects              # auto-named "projects" on first run
qi delete projects               # remove collection data + config entry
```
```

- [ ] **Step 4: Update `CLAUDE.md`**

In `CLAUDE.md`, add a note about auto-naming to the existing `## Key Design Decisions` section. After the existing bullet points, add:

```markdown
- **Auto-named collections**: `qi index` and `qi index <path>` automatically generate a collection name from the directory path (stripping home prefix, replacing `/` with `-`). The name is saved to config on first run. Subsequent runs match by path so the same directory is never indexed under two names. `--name` overrides this for custom names.
```

- [ ] **Step 5: Run full check**

```bash
task check
```

Expected: all checks pass.

- [ ] **Step 6: Commit**

```bash
git add README.md docs/named-collections.md skills/qi-cli/SKILL.md CLAUDE.md
git commit -m "docs: update all docs to reflect auto-named collections"
```
