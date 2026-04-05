package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad_ValidConfig(t *testing.T) {
	path := writeTempConfig(t, `
database_path: /tmp/test.db
collections:
  - name: docs
    path: /tmp
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.DatabasePath != "/tmp/test.db" {
		t.Errorf("unexpected db path: %s", cfg.DatabasePath)
	}
	if len(cfg.Collections) != 1 || cfg.Collections[0].Name != "docs" {
		t.Errorf("unexpected collections: %+v", cfg.Collections)
	}
}

func TestLoad_Defaults(t *testing.T) {
	path := writeTempConfig(t, `
collections:
  - name: notes
    path: /tmp
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Search.BM25TopK != 50 {
		t.Errorf("expected default BM25TopK=50, got %d", cfg.Search.BM25TopK)
	}
	if cfg.Search.RRFK != 60 {
		t.Errorf("expected default RRFK=60, got %d", cfg.Search.RRFK)
	}
}

func TestLoad_DuplicateCollection(t *testing.T) {
	path := writeTempConfig(t, `
collections:
  - name: docs
    path: /tmp
  - name: docs
    path: /var
`)
	_, err := Load(path)
	if err == nil {
		t.Error("expected error for duplicate collection name")
	}
}

func TestLoad_MissingPath(t *testing.T) {
	path := writeTempConfig(t, `
collections:
  - name: docs
`)
	_, err := Load(path)
	if err == nil {
		t.Error("expected error for collection missing path")
	}
}

func TestLoad_RelativePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
collections:
  - name: docs
    path: ./subdir
`), 0o640); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	expected := filepath.Join(dir, "subdir")
	if cfg.Collections[0].Path != expected {
		t.Errorf("expected %q, got %q", expected, cfg.Collections[0].Path)
	}
}

func TestExpandHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	tests := []struct {
		input    string
		expected string
	}{
		{"~/foo", filepath.Join(home, "foo")},
		{"/absolute", "/absolute"},
		{"relative", "relative"},
	}
	for _, tt := range tests {
		got := ExpandHome(tt.input)
		if got != tt.expected {
			t.Errorf("ExpandHome(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
