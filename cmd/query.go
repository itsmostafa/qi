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
	queryMode       string
	queryExplain    bool
	queryCollection string
	queryTopK       int
	querySince      string
	queryUntil      string
	querySort       string
)

var queryCmd = &cobra.Command{
	Use:   "query <query>",
	Short: "Semantic search with optional hybrid mode",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		q := strings.Join(args, " ")
		ctx := context.Background()
		a, err := app.New(ctx, cfgFile)
		if err != nil {
			return err
		}
		defer a.Close()

		mode := queryMode
		if !cmd.Flags().Changed("mode") && a.Config.Search.DefaultMode != "" {
			mode = a.Config.Search.DefaultMode
		}

		opts := search.SearchOpts{
			Query:      q,
			Collection: queryCollection,
			TopK:       queryTopK,
			Pool:       a.Config.Search.BM25TopK,
			Mode:       mode,
			Explain:    queryExplain,
			Since:      querySince,
			Until:      queryUntil,
			Sort:       querySort,
		}
		if err := validateSearchOpts(opts); err != nil {
			return err
		}

		var results []search.Result
		switch mode {
		case "lexical":
			results, err = a.BM25.Search(ctx, opts)
		case "hybrid", "deep":
			if a.Hybrid == nil {
				fmt.Fprintln(os.Stderr, "warning: no embedding provider configured, falling back to lexical search")
				results, err = a.BM25.Search(ctx, opts)
			} else {
				results, err = a.Hybrid.Search(ctx, opts)
			}
		default:
			return fmt.Errorf("unknown mode %q: use lexical, hybrid, or deep", mode)
		}
		if err != nil {
			return fmt.Errorf("query failed: %w", err)
		}
		results = search.Finalize(results, opts)

		formatter := output.New(format)
		return formatter.WriteResults(os.Stdout, results)
	},
}

func init() {
	queryCmd.Flags().StringVar(&queryMode, "mode", "hybrid", "search mode: lexical or hybrid (deep is an alias for hybrid); defaults to search.default_mode from config")
	queryCmd.Flags().BoolVar(&queryExplain, "explain", false, "show scoring breakdown")
	queryCmd.Flags().StringVarP(&queryCollection, "collection", "c", "", "limit to a specific collection")
	queryCmd.Flags().IntVarP(&queryTopK, "limit", "n", 10, "number of results to return")
	addRecencyFlags(queryCmd, &querySince, &queryUntil, &querySort)
}
