package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/itsmostafa/qi/internal/app"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all named collections",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		a, err := app.New(ctx, cfgFile)
		if err != nil {
			return err
		}
		defer a.Close()

		cols := a.Config.Collections
		if len(cols) == 0 {
			fmt.Println("No named collections found.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tPATH")
		for _, c := range cols {
			fmt.Fprintf(w, "%s\t%s\n", c.Name, c.Path)
		}
		return w.Flush()
	},
}
