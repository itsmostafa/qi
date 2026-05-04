package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/itsmostafa/qi/internal/app"
	"github.com/itsmostafa/qi/internal/output"
	"github.com/spf13/cobra"
)

var (
	askCollection string
)

var askCmd = &cobra.Command{
	Use:   "ask <question>",
	Short: "Ask a question and get an LLM-generated answer with citations",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		question := strings.Join(args, " ")
		ctx := context.Background()
		a, err := app.New(ctx, cfgFile)
		if err != nil {
			return err
		}
		defer a.Close()

		if a.Asker == nil {
			return fmt.Errorf("no generation provider configured — add a [providers.generation] section to your config")
		}

		result, err := a.Asker.Ask(ctx, question, askCollection)
		if err != nil {
			return fmt.Errorf("ask failed: %w", err)
		}

		return output.WriteAskResult(os.Stdout, result.Answer, result.Sources, a.Config.Collections, format)
	},
}

func init() {
	askCmd.Flags().StringVarP(&askCollection, "collection", "c", "", "limit context to a specific collection")
}
