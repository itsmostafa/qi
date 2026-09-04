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
	Short: "Check configuration, database, collection paths, and embedding coverage",
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

		// Warn only: repairing modes behind the user's back is worse than saying so.
		warnPermissive := func(label, path string) {
			info, err := os.Stat(path)
			if err != nil {
				return
			}
			if mode := info.Mode().Perm(); mode&0o077 != 0 {
				fmt.Printf("  WARN  %s is readable by other local users (%#o): chmod 600 %s\n", label, mode, path)
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
		warnPermissive("config file", cfgPath)

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
		for _, p := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
			warnPermissive("database file", p)
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
		}

		if cfg != nil && database != nil && cfg.Providers.Embedding != nil {
			health, err := database.EmbeddingHealth(ctx, cfg.Providers.Embedding.Fingerprint(), cfg.Providers.Embedding.Dimension, "")
			if err != nil {
				fmt.Printf("  WARN  embedding health: could not check (%v)\n", err)
				ok = false
			} else {
				status := "OK"
				if health.Missing+health.Stale+health.Orphaned > 0 {
					status = "WARN"
					ok = false
				}
				fmt.Printf("  %-4s  embeddings: %d current / %d missing / %d stale / %d orphaned\n",
					status, health.Current, health.Missing, health.Stale, health.Orphaned)
				if status == "WARN" && cfg.Providers.Embedding != nil {
					fmt.Println("        run `qi index` to repair missing, stale, or orphaned embeddings")
				}
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
