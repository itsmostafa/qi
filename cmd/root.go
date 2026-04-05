package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var (
	cfgFile string
	verbose bool
	format  string
)

var rootCmd = &cobra.Command{
	Use:          "qi",
	Short:        "Local-first knowledge search",
	Long:         `qi indexes your local documents and lets you search, query, and ask questions using BM25 and vector search.`,
	SilenceUsage: true,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ~/.config/qi/config.yaml)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().StringVarP(&format, "format", "f", "text", "output format: text, json, markdown")

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(indexCmd)
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(queryCmd)
	rootCmd.AddCommand(askCmd)
	rootCmd.AddCommand(getCmd)
	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(statsCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(deleteCmd)
}
