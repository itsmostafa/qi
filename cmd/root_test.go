package cmd

import (
	"bytes"
	"log"
	"log/slog"
	"strings"
	"testing"
)

// --verbose parsed but was read by nothing, so slog.Debug output was
// unreachable from the CLI.
func TestVerboseEnablesDebugLogging(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(nil) })

	prev := slog.SetLogLoggerLevel(slog.LevelInfo)
	t.Cleanup(func() { slog.SetLogLoggerLevel(prev) })
	t.Cleanup(func() { verbose = false })

	verbose = false
	rootCmd.PersistentPreRun(rootCmd, nil)
	slog.Debug("quiet-marker")
	if strings.Contains(buf.String(), "quiet-marker") {
		t.Error("debug output logged without --verbose")
	}

	verbose = true
	rootCmd.PersistentPreRun(rootCmd, nil)
	slog.Debug("verbose-marker")
	if !strings.Contains(buf.String(), "verbose-marker") {
		t.Error("--verbose did not enable debug logging")
	}
}

func TestVersionShorthandIsNotVerbose(t *testing.T) {
	if f := rootCmd.PersistentFlags().Lookup("verbose"); f == nil || f.Shorthand != "" {
		t.Error("--verbose must not claim -v; -v is --version")
	}
	if f := rootCmd.Flags().Lookup("version"); f == nil || f.Shorthand != "v" {
		t.Error("-v should be --version")
	}
}
