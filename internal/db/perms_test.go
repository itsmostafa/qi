package db

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// The database and its WAL sidecars hold the indexed corpus; a fresh qi.db used
// to land at 0644 under the default umask.
func TestOpenCreatesPrivateFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "qi.db")
	database, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	// -wal must exist by the end of Open: that is what lets Open tighten it
	// before any document text is written through it.
	for _, p := range []string{path, path + "-wal"} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("%s: %v", filepath.Base(p), err)
		}
		if perm := info.Mode().Perm(); perm&0o077 != 0 {
			t.Errorf("%s mode = %#o, want no group/world bits", filepath.Base(p), perm)
		}
	}
}
