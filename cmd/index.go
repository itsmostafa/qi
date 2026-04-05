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
			cfgPath := cfgFile
			if cfgPath == "" {
				cfgPath = config.DefaultConfigPath()
			}
			// If the path is already registered under a different name, rename it
			// instead of creating a duplicate entry.
			if existing := findCollectionByPath(a.Config.Collections, dir); existing != nil && existing.Name != indexName {
				if err := config.RenameCollection(cfgPath, existing.Name, indexName); err != nil {
					return fmt.Errorf("renaming collection %q → %q: %w", existing.Name, indexName, err)
				}
				fmt.Printf("Renamed collection %q → %q\n", existing.Name, indexName)
			}
			col := config.Collection{Name: indexName, Path: dir}
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

// autoCollection returns the existing collection for absPath if one is already
// registered in config (matched by path), or generates a slug name, saves it
// to config, and returns the new collection.
func autoCollection(a *app.App, absPath string) (config.Collection, error) {
	if _, err := os.Stat(absPath); err != nil {
		return config.Collection{}, fmt.Errorf("path %q does not exist", absPath)
	}
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
