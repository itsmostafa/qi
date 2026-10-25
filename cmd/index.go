package cmd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
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
		var removed []config.Collection
		for _, arg := range args {
			col, err := previewCollection(cfg, arg)
			if err != nil {
				return err
			}
			// Decide before waiting on another run: the hook passes ORIG_HEAD,
			// which the next pull overwrites while this run waits.
			if indexChangedSince != "" {
				// A missing root is only taken as deleted on git's word: an
				// unmounted volume looks the same to stat, and deactivated
				// documents are dropped for good by the next run.
				if _, err := os.Stat(col.Path); errors.Is(err, fs.ErrNotExist) {
					if rootRemovedSince(ctx, col.Path, indexChangedSince) {
						removed = append(removed, col)
					} else {
						fmt.Printf("Skipping %q: %s does not exist (qi delete %s removes it)\n", col.Name, col.Path, col.Name)
					}
					continue
				}
				if !changedSince(ctx, col, indexChangedSince) {
					continue
				}
			}
			targets = append(targets, arg)
		}
		if len(targets) == 0 && len(removed) == 0 {
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
		for _, col := range removed {
			n, err := a.DB.DeactivateCollection(ctx, col.Name)
			if err != nil {
				return fmt.Errorf("deactivating collection %q: %w", col.Name, err)
			}
			fmt.Printf("Removed %d documents from %q: %s was deleted since %s\n", n, col.Name, col.Path, indexChangedSince)
		}
		var cols []config.Collection
		seen := map[string]bool{}
		for _, arg := range targets {
			col, err := resolveCollection(a, arg)
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
// hook install to a collection. A path is registered as a new collection if
// config has none for it.
func resolveCollection(a *app.App, arg string) (config.Collection, error) {
	col, err := previewCollection(a.Config, arg)
	if err != nil || !isPathArg(arg) {
		return col, err
	}
	return autoCollection(a, col.Path)
}

// previewCollection resolves arg like resolveCollection but never writes
// config: a path with no collection yet gets a stand-in with the name it would
// be saved under, carrying the default extensions.
func previewCollection(cfg *config.Config, arg string) (config.Collection, error) {
	if !isPathArg(arg) {
		return collectionByName(cfg, arg)
	}
	dir, err := resolveIndexPath(arg)
	if err != nil {
		return config.Collection{}, fmt.Errorf("resolving path: %w", err)
	}
	// A registered collection resolves even when its root is gone, so a pull
	// that deleted it can deactivate its documents.
	if existing := findCollectionByPath(cfg.Collections, dir); existing != nil {
		return *existing, nil
	}
	if _, err := os.Stat(dir); err != nil {
		return config.Collection{}, fmt.Errorf("path %q does not exist", dir)
	}
	return config.Collection{Name: newCollectionName(cfg.Collections, dir), Path: dir}, nil
}

// newCollectionName names a collection for absPath alongside the existing
// ones, or a colliding basename would be written to config and then
// disambiguated differently in memory on every load.
func newCollectionName(collections []config.Collection, absPath string) string {
	paths := make([]string, 0, len(collections)+1)
	for _, c := range collections {
		paths = append(paths, c.Path)
	}
	paths = append(paths, absPath)
	return config.AssignCollectionNames(paths)[len(paths)-1]
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
	slug := newCollectionName(a.Config.Collections, absPath)
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
	changed := true
	if err == nil {
		changed, err = docsChangedSince(ctx, col, sha)
	}
	switch {
	case err == nil && !changed:
		fmt.Printf("Skipping %q: no indexed files changed since %s\n", col.Name, rev)
	case errors.Is(err, errUnknownRevision):
		fmt.Printf("No %s to compare with yet, as after a fresh clone: running a full index of %q\n", rev, col.Name)
	case err != nil:
		fmt.Printf("Cannot compare %q with %s, indexing everything: %v\n", col.Name, rev, err)
	}
	return changed || err != nil
}

// resolveRevision pins rev to a commit hash in the repository holding dir.
func resolveRevision(ctx context.Context, dir, rev string) (string, error) {
	if strings.HasPrefix(rev, "-") {
		return "", fmt.Errorf("invalid revision %q", rev)
	}
	out, err := gitOutput(ctx, dir, "rev-parse", "-q", "--verify", rev+"^{commit}")
	if err != nil {
		// --verify -q exits 1 silently for a name that resolves to nothing;
		// anything else (not a repository, no git) says why on stderr, and
		// gitOutput keeps the exit error only when stderr is empty.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return "", errUnknownRevision
		}
		return "", fmt.Errorf("git rev-parse: %w", err)
	}
	return out, nil
}

// rootRemovedSince reports whether root, missing on disk, is a path git tracked
// at rev and no longer tracks at HEAD: the change from rev deleted it. Git runs
// from root's nearest existing ancestor, since `git -C` needs a directory.
func rootRemovedSince(ctx context.Context, root, rev string) bool {
	dir, rel := root, "."
	for {
		if _, err := os.Stat(dir); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		rel = filepath.Join(filepath.Base(dir), rel)
		dir = parent
	}
	sha, err := resolveRevision(ctx, dir, rev)
	if err != nil {
		return false
	}
	// ls-tree paths are relative to the working directory, like rel.
	tracked := func(commit string) bool {
		out, err := gitOutput(ctx, dir, "ls-tree", "--name-only", commit, "--", rel)
		return err == nil && out != ""
	}
	return tracked(sha) && !tracked("HEAD")
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
	// rev is a hash from resolveRevision, which refuses option-like input.
	// gitOutput's trimming is safe: the output starts with ':' and ends with NUL.
	out, err := gitOutput(ctx, col.Path, "diff", "--raw", "-z", "--no-renames",
		"--no-abbrev", "--ignore-submodules=none", rev, "HEAD", "--", ".")
	if err != nil {
		return false, fmt.Errorf("git diff: %w", err)
	}
	// Each entry is ":srcmode dstmode srcsha dstsha status" NUL path NUL.
	exts := indexer.AllowedExtensions(col)
	fields := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	for i := 0; i+1 < len(fields); i += 2 {
		modes := strings.Fields(strings.TrimPrefix(fields[i], ":"))
		if len(modes) >= 2 && (modes[0] == gitlinkMode || modes[1] == gitlinkMode) {
			return true, nil
		}
		if exts[strings.ToLower(filepath.Ext(fields[i+1]))] {
			return true, nil
		}
	}
	return false, nil
}
