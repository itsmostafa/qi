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

		targetName, inConfig, err := resolveDeleteTarget(a.Config.Collections, name)
		if err != nil {
			return err
		}

		inDB, err := collectionExistsInDB(ctx, a.DB, targetName)
		if err != nil {
			return fmt.Errorf("checking collection: %w", err)
		}
		if !inConfig && !inDB {
			return fmt.Errorf("collection %q not found", name)
		}

		// Config first: a failure here leaves the data intact and the command
		// repeatable. The other order deletes the documents and then reports an
		// error, with the collection still listed and nothing left to delete.
		if inConfig {
			cfgPath := cfgFile
			if cfgPath == "" {
				cfgPath = config.DefaultConfigPath()
			}
			if err := config.RemoveCollection(cfgPath, targetName); err != nil {
				return fmt.Errorf("removing collection from config: %w", err)
			}
		}

		if err := a.DB.DeleteCollection(ctx, targetName); err != nil {
			return fmt.Errorf("deleting collection data: %w", err)
		}

		fmt.Printf("Deleted collection %q\n", targetName)
		return nil
	},
}

func resolveDeleteTarget(collections []config.Collection, name string) (string, bool, error) {
	for _, c := range collections {
		if c.Name == name {
			return c.Name, true, nil
		}
	}

	targetName := ""
	matches := 0
	for _, c := range collections {
		if c.OriginalName == name {
			targetName = c.Name
			matches++
		}
	}
	if matches > 1 {
		return "", false, fmt.Errorf("collection %q is ambiguous; matches multiple legacy names", name)
	}
	if matches == 1 {
		return targetName, true, nil
	}
	return name, false, nil
}

func collectionExistsInDB(ctx context.Context, database *db.DB, name string) (bool, error) {
	var exists int
	err := database.QueryRowContext(ctx, `
		SELECT CASE WHEN
			EXISTS (SELECT 1 FROM documents WHERE collection = ?)
			OR EXISTS (SELECT 1 FROM index_runs WHERE collection = ?)
		THEN 1 ELSE 0 END
	`, name, name).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists != 0, nil
}
