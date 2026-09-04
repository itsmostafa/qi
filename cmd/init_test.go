package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// `qi --config x.yaml init` used to initialize the global default database.
func TestInitUsesSelectedConfigDatabasePath(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "custom.db")
	cfgPath := writeIndexTestConfig(t, fmt.Sprintf("database_path: %s\n", dbPath))
	withIndexTestConfig(t, cfgPath)

	var runErr error
	output := captureIndexTestOutput(t, func() { runErr = initCmd.RunE(initCmd, nil) })
	if runErr != nil {
		t.Fatalf("init failed: %v\n%s", runErr, output)
	}

	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("init did not create the config's database: %v\n%s", err, output)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("database mode = %#o, want 0600", perm)
	}
}
