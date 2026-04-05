package cmd

import (
	"context"
	"fmt"

	"github.com/itsmostafa/qi/internal/app"
	"github.com/itsmostafa/qi/internal/config"
	"github.com/spf13/cobra"
)

var deleteCmd = &cobra.Command{
	Use:   "delete <collection>",
	Short: "Delete a named collection and all its indexed data",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		ctx := context.Background()
		a, err := app.New(ctx, cfgFile)
		if err != nil {
			return err
		}
		defer a.Close()

		// Verify the collection exists in config before doing anything.
		found := false
		for _, c := range a.Config.Collections {
			if c.Name == name {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("collection %q not found", name)
		}

		if err := a.DB.DeleteCollection(ctx, name); err != nil {
			return fmt.Errorf("deleting collection data: %w", err)
		}

		cfgPath := cfgFile
		if cfgPath == "" {
			cfgPath = config.DefaultConfigPath()
		}
		if err := config.RemoveCollection(cfgPath, name); err != nil {
			return fmt.Errorf("removing collection from config: %w", err)
		}

		fmt.Printf("Deleted collection %q\n", name)
		return nil
	},
}
