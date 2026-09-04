package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/itsmostafa/qi/internal/version"
	"github.com/spf13/cobra"
)

var (
	cfgFile     string
	verbose     bool
	format      string
	showVersion bool
)

var rootCmd = &cobra.Command{
	Use:          "qi",
	Short:        "Local-first knowledge search",
	Long:         `qi indexes your local documents and lets you search and query them using BM25 and vector search.`,
	SilenceUsage: true,
	// --verbose was parsed but never read, so debug logging was unreachable.
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if verbose {
			slog.SetLogLoggerLevel(slog.LevelDebug)
		}
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if showVersion {
			fmt.Println(version.String())
			return nil
		}
		return cmd.Help()
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ~/.config/qi/config.yaml)")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "log debug detail to stderr")
	rootCmd.PersistentFlags().StringVarP(&format, "format", "f", "text", "output format: text, json, markdown")
	rootCmd.Flags().BoolVarP(&showVersion, "version", "v", false, "print version")

	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(indexCmd)
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(queryCmd)
	rootCmd.AddCommand(getCmd)
	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(statsCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(deleteCmd)
}
