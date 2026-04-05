package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/itsmostafa/qi/internal/app"
	"github.com/itsmostafa/qi/internal/config"
)

func TestFindCollectionByPath(t *testing.T) {
	cols := []config.Collection{
		{Name: "notes", Path: "/home/user/notes"},
		{Name: "code", Path: "/home/user/projects"},
	}

	t.Run("found", func(t *testing.T) {
		got := findCollectionByPath(cols, "/home/user/notes")
		if got == nil {
			t.Fatal("expected non-nil result")
		}
		if got.Name != "notes" {
			t.Errorf("expected name %q, got %q", "notes", got.Name)
		}
	})

	t.Run("not found", func(t *testing.T) {
		got := findCollectionByPath(cols, "/home/user/other")
		if got != nil {
			t.Errorf("expected nil, got %+v", got)
		}
	})

	t.Run("empty slice", func(t *testing.T) {
		got := findCollectionByPath(nil, "/home/user/notes")
		if got != nil {
			t.Errorf("expected nil on empty slice, got %+v", got)
		}
	})
}

func TestIsPathArg(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"/absolute/path", true},
		{"./relative", true},
		{"../parent", true},
		{"~", true},
		{"~/home", true},
		{".", true},
		{"..", true},
		{"notes", false},
		{"my-collection", false},
		{"", false},
	}
	for _, tt := range tests {
		got := isPathArg(tt.input)
		if got != tt.expected {
			t.Errorf("isPathArg(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestAutoCollection(t *testing.T) {
	// Write a minimal config with a temp DB path.
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfgContent := fmt.Sprintf("database_path: %s\n", dbPath)
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o640); err != nil {
		t.Fatal(err)
	}

	// Override the global cfgFile used by commands.
	origCfgFile := cfgFile
	cfgFile = cfgPath
	t.Cleanup(func() { cfgFile = origCfgFile })

	ctx := context.Background()

	t.Run("miss path: new collection auto-saved", func(t *testing.T) {
		a, err := app.New(ctx, cfgPath)
		if err != nil {
			t.Fatalf("app.New: %v", err)
		}
		defer a.Close()

		absPath := tmpDir
		col, err := autoCollection(a, absPath)
		if err != nil {
			t.Fatalf("autoCollection: %v", err)
		}
		if col.Path != absPath {
			t.Errorf("col.Path = %q, want %q", col.Path, absPath)
		}
		if col.Name == absPath {
			t.Error("col.Name must not be the raw path")
		}
		if col.Name == "" {
			t.Error("col.Name must not be empty")
		}
		// Verify it was written to the config file.
		data, _ := os.ReadFile(cfgPath)
		if !strings.Contains(string(data), col.Name) {
			t.Errorf("config file does not contain collection name %q", col.Name)
		}
	})

	t.Run("hit path: existing collection returned without re-writing", func(t *testing.T) {
		// Pre-populate config with a collection for tmpDir.
		existingSlug := "existing-collection"
		cfgContent2 := fmt.Sprintf("database_path: %s\ncollections:\n  - name: %s\n    path: %s\n", dbPath, existingSlug, tmpDir)
		if err := os.WriteFile(cfgPath, []byte(cfgContent2), 0o640); err != nil {
			t.Fatal(err)
		}

		a, err := app.New(ctx, cfgPath)
		if err != nil {
			t.Fatalf("app.New: %v", err)
		}
		defer a.Close()

		col, err := autoCollection(a, tmpDir)
		if err != nil {
			t.Fatalf("autoCollection: %v", err)
		}
		if col.Name != existingSlug {
			t.Errorf("col.Name = %q, want %q (existing collection should be reused)", col.Name, existingSlug)
		}
	})
}
