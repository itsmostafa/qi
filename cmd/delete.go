package cmd

import (
	"context"
	"fmt"

	"github.com/itsmostafa/qi/internal/app"
	"github.com/itsmostafa/qi/internal/config"
	"github.com/itsmostafa/qi/internal/db"
	"github.com/spf13/cobra"
)

var deleteCmd = &cobra.Command{
	Use:   "delete <collection>",
	Short: "Delete a collection and all its indexed data",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		ctx := context.Background()
		a, err := app.New(ctx, cfgFile)
		if err != nil {
			return err
		}
		defer a.Close()

		inConfig := false
		for _, c := range a.Config.Collections {
			if c.Name == name {
				inConfig = true
				break
			}
		}

		inDB, err := collectionExistsInDB(ctx, a.DB, name)
		if err != nil {
			return fmt.Errorf("checking collection: %w", err)
		}
		if !inConfig && !inDB {
			return fmt.Errorf("collection %q not found", name)
		}

		if err := a.DB.DeleteCollection(ctx, name); err != nil {
			return fmt.Errorf("deleting collection data: %w", err)
		}

		if inConfig {
			cfgPath := cfgFile
			if cfgPath == "" {
				cfgPath = config.DefaultConfigPath()
			}
			if err := config.RemoveCollection(cfgPath, name); err != nil {
				return fmt.Errorf("removing collection from config: %w", err)
			}
		}

		fmt.Printf("Deleted collection %q\n", name)
		return nil
	},
}

func collectionExistsInDB(ctx context.Context, database *db.DB, name string) (bool, error) {
	var exists int
	err := database.QueryRowContext(ctx, `
		SELECT CASE WHEN
			EXISTS (SELECT 1 FROM documents WHERE collection = ?)
			OR EXISTS (SELECT 1 FROM index_runs WHERE collection = ?)
			OR EXISTS (SELECT 1 FROM collections WHERE name = ?)
		THEN 1 ELSE 0 END
	`, name, name, name).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists != 0, nil
}
