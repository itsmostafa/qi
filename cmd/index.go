package cmd

import (
	"context"
	"fmt"

	"github.com/itsmostafa/qi/internal/app"
	"github.com/spf13/cobra"
)

var indexCmd = &cobra.Command{
	Use:   "index [collection]",
	Short: "Index documents into the knowledge base",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		a, err := app.New(ctx, cfgFile)
		if err != nil {
			return err
		}
		defer a.Close()

		collections := a.Config.Collections
		if len(args) > 0 {
			name := args[0]
			collections = nil
			for _, c := range a.Config.Collections {
				if c.Name == name {
					collections = append(collections, c)
					break
				}
			}
			if collections == nil {
				return fmt.Errorf("collection %q not found in config", name)
			}
		}

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
	},
}
