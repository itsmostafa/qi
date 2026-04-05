package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/itsmostafa/qi/internal/config"
	"github.com/itsmostafa/qi/internal/db"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check configuration, database, and provider health",
	RunE: func(cmd *cobra.Command, args []string) error {
		ok := true
		check := func(label string, err error) {
			if err != nil {
				fmt.Printf("  FAIL  %s: %v\n", label, err)
				ok = false
			} else {
				fmt.Printf("  OK    %s\n", label)
			}
		}

		fmt.Println("qi doctor")
		fmt.Println()

		// Config
		cfgPath := cfgFile
		if cfgPath == "" {
			cfgPath = config.DefaultConfigPath()
		}
		_, statErr := os.Stat(cfgPath)
		check("config file exists", statErr)

		var cfg *config.Config
		if statErr == nil {
			var err error
			cfg, err = config.Load(cfgPath)
			check("config parses", err)
		}

		// Database
		dbPath := config.DefaultDBPath()
		if cfg != nil && cfg.DatabasePath != "" {
			dbPath = cfg.DatabasePath
		}
		ctx := context.Background()
		database, err := db.Open(ctx, dbPath)
		if err != nil {
			check("database opens", err)
		} else {
			defer database.Close()
			check("database opens", nil)
			check("database ping", database.Ping(ctx))
		}

		// Collections
		if cfg != nil {
			for _, col := range cfg.Collections {
				_, err := os.Stat(col.Path)
				check(fmt.Sprintf("collection %q path exists", col.Name), err)
			}
		}

		// Providers
		if cfg != nil {
			if cfg.Providers.Embedding == nil {
				fmt.Println("  SKIP  embedding provider (not configured)")
			}
			if cfg.Providers.Generation == nil {
				fmt.Println("  SKIP  generation provider (not configured)")
			}
		}

		fmt.Println()
		if ok {
			fmt.Println("All checks passed.")
		} else {
			fmt.Println("Some checks failed. Run `qi init` to set up missing components.")
			return fmt.Errorf("doctor found issues")
		}
		return nil
	},
}
