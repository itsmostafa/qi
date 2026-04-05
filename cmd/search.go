package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/itsmostafa/qi/internal/app"
	"github.com/itsmostafa/qi/internal/output"
	"github.com/itsmostafa/qi/internal/search"
	"github.com/spf13/cobra"
)

var (
	searchCollection string
	searchTopK       int
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Full-text search using BM25",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		query := strings.Join(args, " ")
		ctx := context.Background()
		a, err := app.New(ctx, cfgFile)
		if err != nil {
			return err
		}
		defer a.Close()

		opts := search.SearchOpts{
			Query:      query,
			Collection: searchCollection,
			TopK:       searchTopK,
			Mode:       "lexical",
		}

		results, err := a.BM25.Search(ctx, opts)
		if err != nil {
			return fmt.Errorf("search failed: %w", err)
		}

		formatter := output.New(format)
		return formatter.WriteResults(os.Stdout, results)
	},
}

func init() {
	searchCmd.Flags().StringVarP(&searchCollection, "collection", "c", "", "limit to a specific collection")
	searchCmd.Flags().IntVarP(&searchTopK, "limit", "n", 10, "number of results to return")
}
