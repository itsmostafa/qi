package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/itsmostafa/qi/internal/app"
	"github.com/itsmostafa/qi/internal/config"
	"github.com/itsmostafa/qi/internal/db"
	"github.com/itsmostafa/qi/internal/indexer"
	"github.com/spf13/cobra"
)

var (
	indexForce        bool
	indexChangedSince string
)

var indexCmd = &cobra.Command{
	Use:   "index [path|collection]...",
	Short: "Index documents into the knowledge base",
	Long: `Index documents from one or more directories or collections.

With no arguments, indexes the current directory (named from path).
With a path argument (absolute, relative, or starting with ~), indexes that directory (named from path).
With a collection name, indexes the collection from config.
Several paths and collection names can be given at once; each is indexed in turn.

Unchanged files are skipped by content hash. After upgrading qi, use --force to
rebuild chunks and embeddings with the current parser.

A collection name is derived automatically from the directory path:
  /Users/alice/Projects/tools/qi -> qi
Colliding names take on leading path segments until unique.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		cfg, err := config.Load(cfgFile)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		if len(args) == 0 {
			args = []string{"."}
		}
		// Look each argument up without saving anything: registering a new or
		// renamed collection waits for app.New, which first moves a legacy
		// name's rows to the normalized name.
		var targets []string
		for _, arg := range args {
			col, err := previewCollection(cfg, arg)
			if err != nil {
				return err
			}
			// Decide before waiting on another run: the hook passes ORIG_HEAD,
			// which the next pull overwrites while this run waits.
			if indexChangedSince != "" && !changedSince(ctx, col, indexChangedSince) {
				continue
			}
			targets = append(targets, arg)
		}
		if len(targets) == 0 {
			return nil
		}
		unlock, err := db.LockIndex(cfg.DatabasePath, func() {
			fmt.Printf("Waiting for another qi index run on %s to finish...\n", cfg.DatabasePath)
		})
		if err != nil {
			return err
		}
		defer unlock()

		a, err := app.New(ctx, cfgFile)
		if err != nil {
			return err
		}
		defer a.Close()
		var cols []config.Collection
		seen := map[string]bool{}
		for _, arg := range targets {
			col, err := resolveCollection(a, []string{arg})
			if err != nil {
				return err
			}
			if key := canonicalIndexPath(col.Path); !seen[key] {
				seen[key] = true
				cols = append(cols, col)
			}
		}
		return runIndex(ctx, a, cols)
	},
}

// resolveCollection maps the [path|collection] argument shared by index and
// hook install to a collection. A path (or no argument, meaning the current
// directory) is registered as a new collection if config has none for it.
func resolveCollection(a *app.App, args []string) (config.Collection, error) {
	if len(args) == 0 || isPathArg(args[0]) {
		arg := "."
		if len(args) > 0 {
			arg = args[0]
		}
		dir, err := resolveIndexPath(arg)
		if err != nil {
			return config.Collection{}, fmt.Errorf("resolving path: %w", err)
		}
		return autoCollection(a, dir)
	}
	return collectionByName(a.Config, args[0])
}

// previewCollection resolves arg like resolveCollection but never writes
// config: a path with no collection yet gets a stand-in named after its
// directory, carrying the default extensions.
func previewCollection(cfg *config.Config, arg string) (config.Collection, error) {
	if !isPathArg(arg) {
		return collectionByName(cfg, arg)
	}
	dir, err := resolveIndexPath(arg)
	if err != nil {
		return config.Collection{}, fmt.Errorf("resolving path: %w", err)
	}
	if _, err := os.Stat(dir); err != nil {
		return config.Collection{}, fmt.Errorf("path %q does not exist", dir)
	}
	if existing := findCollectionByPath(cfg.Collections, dir); existing != nil {
		return *existing, nil
	}
	return config.Collection{Name: filepath.Base(dir), Path: dir}, nil
}

func collectionByName(cfg *config.Config, name string) (config.Collection, error) {
	for _, c := range cfg.Collections {
		if c.Name == name || c.OriginalName == name {
			return c, nil
		}
	}
	return config.Collection{}, fmt.Errorf("collection %q not found in config", name)
}

// autoCollection returns the existing collection for absPath if one is already
// registered in config (matched by path), or generates a slug name, saves it
// to config, and returns the new collection.
func autoCollection(a *app.App, absPath string) (config.Collection, error) {
	if _, err := os.Stat(absPath); err != nil {
		return config.Collection{}, fmt.Errorf("path %q does not exist", absPath)
	}
	if existing := findCollectionByPath(a.Config.Collections, absPath); existing != nil {
		if existing.OriginalName != "" {
			if err := saveCollection(*existing); err != nil {
				return config.Collection{}, err
			}
			fmt.Printf("Updated collection %q -> %s\n", existing.Name, existing.Path)
			existing.OriginalName = ""
		}
		return *existing, nil
	}
	// Name the new collection alongside the existing ones, or a colliding
	// basename would be written to config and then disambiguated differently
	// in memory on every load.
	paths := make([]string, 0, len(a.Config.Collections)+1)
	for _, c := range a.Config.Collections {
		paths = append(paths, c.Path)
	}
	paths = append(paths, absPath)
	slug := config.AssignCollectionNames(paths)[len(paths)-1]
	col := config.Collection{Name: slug, Path: absPath}
	if err := saveCollection(col); err != nil {
		return config.Collection{}, err
	}
	fmt.Printf("Saved collection %q -> %s\n", slug, absPath)
	a.Config.Collections = append(a.Config.Collections, col)
	return col, nil
}

func saveCollection(col config.Collection) error {
	cfgPath := cfgFile
	if cfgPath == "" {
		cfgPath = config.DefaultConfigPath()
	}
	if err := config.AddCollection(cfgPath, col); err != nil {
		return fmt.Errorf("saving collection to config: %w", err)
	}
	return nil
}

// findCollectionByPath returns a pointer to the first collection whose Path
// equals absPath, or nil if none matches.
func findCollectionByPath(collections []config.Collection, absPath string) *config.Collection {
	absPath = canonicalIndexPath(absPath)
	for i := range collections {
		if canonicalIndexPath(collections[i].Path) == absPath {
			return &collections[i]
		}
	}
	return nil
}

func resolveIndexPath(path string) (string, error) {
	return config.CanonicalPath(path)
}

func canonicalIndexPath(path string) string {
	canonical, err := config.CanonicalPath(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return canonical
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
	a.Indexer.Force = indexForce
	var collectionErrs []error
	for _, col := range collections {
		fmt.Printf("Indexing %q (%s)...\n", col.Name, col.Path)
		stats, err := a.Indexer.Index(ctx, col)
		if err != nil {
			err = fmt.Errorf("indexing collection %q: %w", col.Name, err)
			fmt.Printf("  error: %v\n", err)
			collectionErrs = append(collectionErrs, err)
			continue
		}
		fmt.Printf("  scanned=%d added=%d updated=%d removed=%d skipped=%d time=%s\n",
			stats.FilesScanned, stats.FilesAdded, stats.FilesUpdated, stats.FilesRemoved, stats.FilesSkipped, stats.Duration.Round(1000000))
		if a.Embedder != nil {
			fmt.Printf("  embedding chunks...\n")
			if err := a.Embedder.EmbedCollection(ctx, col.Name); err != nil {
				err = fmt.Errorf("embedding collection %q: %w", col.Name, err)
				fmt.Printf("  error: %v\n", err)
				collectionErrs = append(collectionErrs, err)
			}
		}
	}
	return errors.Join(collectionErrs...)
}

func init() {
	indexCmd.Flags().BoolVar(&indexForce, "force", false,
		"reindex files whose content is unchanged (needed after a qi upgrade changes parsing)")
	indexCmd.Flags().StringVar(&indexChangedSince, "changed-since", "",
		"skip the run unless a file qi indexes changed between this git revision and HEAD")
}

// errUnknownRevision means git is working but the revision names no commit —
// ORIG_HEAD before the first pull after a clone.
var errUnknownRevision = errors.New("unknown revision")

// changedSince reports whether col needs indexing after HEAD moved from rev,
// printing why when the answer is not a plain yes.
func changedSince(ctx context.Context, col config.Collection, rev string) bool {
	sha, err := resolveRevision(ctx, col.Path, rev)
	if err == nil {
		var changed bool
		if changed, err = docsChangedSince(ctx, col, sha); err == nil {
			if !changed {
				fmt.Printf("Skipping %q: no indexed files changed since %s\n", col.Name, rev)
			}
			return changed
		}
	}
	if errors.Is(err, errUnknownRevision) {
		fmt.Printf("No %s to compare with yet, as after a fresh clone: running a full index of %q\n", rev, col.Name)
	} else {
		fmt.Printf("Cannot compare %q with %s, indexing everything: %v\n", col.Name, rev, err)
	}
	return true
}

// resolveRevision pins rev to a commit hash in the repository holding dir.
func resolveRevision(ctx context.Context, dir, rev string) (string, error) {
	if strings.HasPrefix(rev, "-") {
		return "", fmt.Errorf("invalid revision %q", rev)
	}
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "-q", "--verify", rev+"^{commit}")
	cmd.Env = gitEnv()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		// --verify -q exits 1 silently for a name that resolves to nothing;
		// anything else (not a repository, no git) says why on stderr.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && stderr.Len() == 0 {
			return "", errUnknownRevision
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("git rev-parse: %s", msg)
		}
		return "", fmt.Errorf("git rev-parse: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// gitlinkMode is the tree mode of a submodule entry.
const gitlinkMode = "160000"

// docsChangedSince reports whether any file with one of col's indexed
// extensions, under col.Path, differs between rev and HEAD. Deletions count:
// they deactivate documents. A submodule bump counts too: it shows up as one
// extensionless gitlink path, and the documents it moves are invisible to the
// diff. An error means git could not answer (not a repository, unknown
// revision), and the caller indexes everything.
func docsChangedSince(ctx context.Context, col config.Collection, rev string) (bool, error) {
	if strings.HasPrefix(rev, "-") {
		return false, fmt.Errorf("invalid revision %q", rev)
	}
	cmd := exec.CommandContext(ctx, "git", "-C", col.Path, "diff", "--raw", "-z", "--no-renames",
		"--no-abbrev", "--ignore-submodules=none", rev, "HEAD", "--", ".")
	// Inside a git hook, an inherited GIT_DIR would make col.Path the
	// worktree root, widening "." to the whole repository.
	cmd.Env = gitEnv()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return false, fmt.Errorf("git diff: %s", msg)
		}
		return false, fmt.Errorf("git diff: %w", err)
	}
	// Each entry is ":srcmode dstmode srcsha dstsha status" NUL path NUL.
	exts := indexer.AllowedExtensions(col)
	fields := bytes.Split(bytes.TrimSuffix(out, []byte{0}), []byte{0})
	for i := 0; i+1 < len(fields); i += 2 {
		modes := strings.Fields(strings.TrimPrefix(string(fields[i]), ":"))
		if len(modes) >= 2 && (modes[0] == gitlinkMode || modes[1] == gitlinkMode) {
			return true, nil
		}
		if exts[strings.ToLower(filepath.Ext(string(fields[i+1])))] {
			return true, nil
		}
	}
	return false, nil
}
