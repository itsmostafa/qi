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

var indexCmd = &cobra.Command{
	Use:   "index [path|collection]",
	Short: "Index documents into the knowledge base",
	Long: `Index documents from a directory or named collection.

With no arguments, indexes the current directory.
With a path argument (absolute, relative, or starting with ~), indexes that directory.
With a collection name, indexes the named collection from config.
With no arguments and no path-like arg, indexes all configured collections.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		a, err := app.New(ctx, cfgFile)
		if err != nil {
			return err
		}
		defer a.Close()

		// If arg looks like a path, index it as an ad-hoc collection.
		if len(args) > 0 && isPathArg(args[0]) {
			dir, err := filepath.Abs(config.ExpandHome(args[0]))
			if err != nil {
				return fmt.Errorf("resolving path: %w", err)
			}
			col := config.Collection{Name: dir, Path: dir}
			return runIndex(ctx, a, []config.Collection{col})
		}

		// No args: index current directory.
		if len(args) == 0 {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting current directory: %w", err)
			}
			col := config.Collection{Name: cwd, Path: cwd}
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
