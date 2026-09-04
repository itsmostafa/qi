package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/itsmostafa/qi/internal/app"
	"github.com/itsmostafa/qi/internal/config"
	"github.com/spf13/cobra"
)

var indexForce bool

var indexCmd = &cobra.Command{
	Use:   "index [path|collection]",
	Short: "Index documents into the knowledge base",
	Args:  cobra.MaximumNArgs(1),
	Long: `Index documents from a directory or collection.

With no arguments, indexes the current directory (named from path).
With a path argument (absolute, relative, or starting with ~), indexes that directory (named from path).
With a collection name, indexes the collection from config.

Unchanged files are skipped by content hash. After upgrading qi, use --force to
rebuild chunks and embeddings with the current parser.

A collection name is derived automatically from the directory path:
  /Users/alice/Projects/tools/qi -> qi
Colliding names take on leading path segments until unique.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		a, err := app.New(ctx, cfgFile)
		if err != nil {
			return err
		}
		defer a.Close()

		// If arg looks like a path, index it as a (possibly new) collection.
		if len(args) > 0 && isPathArg(args[0]) {
			dir, err := resolveIndexPath(args[0])
			if err != nil {
				return fmt.Errorf("resolving path: %w", err)
			}
			col, err := autoCollection(a, dir)
			if err != nil {
				return err
			}
			return runIndex(ctx, a, []config.Collection{col})
		}

		// No args: index current directory as a (possibly new) collection.
		if len(args) == 0 {
			cwd, err := resolveIndexPath(".")
			if err != nil {
				return fmt.Errorf("resolving current directory: %w", err)
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
			if c.Name == name || c.OriginalName == name {
				return runIndex(ctx, a, []config.Collection{c})
			}
		}
		return fmt.Errorf("collection %q not found in config", name)
	},
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
}
