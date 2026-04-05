package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/itsmostafa/qi/internal/config"
	"github.com/itsmostafa/qi/internal/db"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize qi configuration and database",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfgPath := cfgFile
		if cfgPath == "" {
			cfgPath = config.DefaultConfigPath()
		}

		// Create config directory
		if err := os.MkdirAll(filepath.Dir(cfgPath), 0o750); err != nil {
			return fmt.Errorf("creating config dir: %w", err)
		}

		// Write default config only if it doesn't exist
		if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
			if err := os.WriteFile(cfgPath, []byte(config.DefaultConfigTemplate), 0o640); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}
			fmt.Printf("Config written to %s\n", cfgPath)
		} else {
			fmt.Printf("Config already exists at %s\n", cfgPath)
		}

		// Open (and migrate) the database
		dbPath := config.DefaultDBPath()
		ctx := context.Background()
		database, err := db.Open(ctx, dbPath)
		if err != nil {
			return fmt.Errorf("opening database: %w", err)
		}
		defer database.Close()

		fmt.Printf("Database initialized at %s\n", dbPath)
		fmt.Println("Run `qi doctor` to verify your setup.")
		return nil
	},
}
