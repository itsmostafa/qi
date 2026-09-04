package config

import (
	"os"
	"path/filepath"
	"strings"
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
	if len(cfg.Collections) != 1 || cfg.Collections[0].Name != SlugFromPath("/tmp") {
		t.Errorf("unexpected collections: %+v", cfg.Collections)
	}
	if cfg.Collections[0].OriginalName != "docs" {
		t.Errorf("expected original collection name to be preserved, got %q", cfg.Collections[0].OriginalName)
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
    path: /tmp/foo bar
  - name: docs
    path: /tmp/foo@bar
`)
	_, err := Load(path)
	if err == nil {
		t.Error("expected error for duplicate generated collection name")
	}
}

func TestLoad_DuplicateCollectionPath(t *testing.T) {
	path := writeTempConfig(t, `
collections:
  - name: docs
    path: /tmp/docs
  - name: notes
    path: /tmp/../tmp/docs
`)
	_, err := Load(path)
	if err == nil {
		t.Error("expected error for duplicate collection path")
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

func TestLoad_OpenAIEnvOverrides(t *testing.T) {
	tests := []struct {
		name        string
		envKey      string
		provider    string
		wantBaseURL string
		wantAPIKey  string
	}{
		{
			name:   "openai fills base_url and key from env",
			envKey: "sk-test-emb",
			provider: `    name: openai
    model: text-embedding-3-small
    dimension: 1536`,
			wantBaseURL: "https://api.openai.com",
			wantAPIKey:  "sk-test-emb",
		},
		{
			name:   "config key wins over env",
			envKey: "sk-from-env",
			provider: `    name: openai
    model: text-embedding-3-small
    dimension: 1536
    api_key: sk-from-config`,
			wantBaseURL: "https://api.openai.com",
			wantAPIKey:  "sk-from-config",
		},
		{
			name:   "explicit base_url is preserved",
			envKey: "sk-test",
			provider: `    name: openai
    model: text-embedding-3-small
    dimension: 1536
    base_url: https://custom.proxy.example.com`,
			wantBaseURL: "https://custom.proxy.example.com",
			wantAPIKey:  "sk-test",
		},
		{
			name:   "env key does not apply to non-openai providers",
			envKey: "sk-should-not-apply",
			provider: `    name: ollama
    base_url: http://localhost:11434
    model: nomic-embed-text
    dimension: 768`,
			wantBaseURL: "http://localhost:11434",
			wantAPIKey:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OPENAI_API_KEY", tt.envKey)
			path := writeTempConfig(t, `
collections:
  - name: docs
    path: /tmp
providers:
  embedding:
`+tt.provider+"\n")
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load failed: %v", err)
			}
			if cfg.Providers.Embedding == nil {
				t.Fatal("expected embedding provider")
			}
			if cfg.Providers.Embedding.BaseURL != tt.wantBaseURL {
				t.Errorf("base_url = %q, want %q", cfg.Providers.Embedding.BaseURL, tt.wantBaseURL)
			}
			if cfg.Providers.Embedding.APIKey != tt.wantAPIKey {
				t.Errorf("api_key = %q, want %q", cfg.Providers.Embedding.APIKey, tt.wantAPIKey)
			}
		})
	}
}

func TestLoad_Rerank_EnvBaseURL(t *testing.T) {
	t.Setenv("RERANK_BASE_URL", "http://reranker.local:8080")
	path := writeTempConfig(t, `
collections:
  - name: docs
    path: /tmp
providers:
  rerank:
    name: jina
    base_url: ${RERANK_BASE_URL}
    model: jina-reranker-v2-base-multilingual
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Providers.Rerank == nil {
		t.Fatal("expected rerank provider")
	}
	if cfg.Providers.Rerank.BaseURL != "http://reranker.local:8080" {
		t.Errorf("expected expanded base_url, got %q", cfg.Providers.Rerank.BaseURL)
	}
}

func TestAddCollectionNormalizesSamePathLegacyName(t *testing.T) {
	dir := t.TempDir()
	collectionPath := filepath.Join(dir, "foo bar")
	if err := os.Mkdir(collectionPath, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := writeTempConfig(t, `
collections:
  - name: legacy
    path: `+collectionPath+`
`)

	if err := AddCollection(configPath, Collection{Path: collectionPath}); err != nil {
		t.Fatalf("AddCollection failed: %v", err)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if got, want := len(cfg.Collections), 1; got != want {
		t.Fatalf("collection count = %d, want %d", got, want)
	}
	if got, want := cfg.Collections[0].Name, SlugFromPath(collectionPath); got != want {
		t.Fatalf("collection name = %q, want %q", got, want)
	}
	if cfg.Collections[0].OriginalName != "" {
		t.Fatalf("legacy name was not normalized in config, got original name %q", cfg.Collections[0].OriginalName)
	}
}

func TestRemoveCollectionPrefersGeneratedNameOverLegacyName(t *testing.T) {
	dir := t.TempDir()
	firstPath := filepath.Join(dir, "alpha")
	secondPath := filepath.Join(dir, "beta")
	for _, path := range []string{firstPath, secondPath} {
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	firstName := SlugFromPath(firstPath)
	secondName := SlugFromPath(secondPath)
	configPath := writeTempConfig(t, `
collections:
  - name: `+secondName+`
    path: `+firstPath+`
  - path: `+secondPath+`
`)

	if err := RemoveCollection(configPath, secondName); err != nil {
		t.Fatalf("RemoveCollection failed: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if got, want := len(cfg.Collections), 1; got != want {
		t.Fatalf("collection count = %d, want %d", got, want)
	}
	if got := cfg.Collections[0].Name; got != firstName {
		t.Fatalf("remaining collection name = %q, want %q", got, firstName)
	}
	if got := cfg.Collections[0].OriginalName; got != secondName {
		t.Fatalf("remaining original name = %q, want %q", got, secondName)
	}
}

func TestAddCollectionRejectsSlugCollisionDifferentPath(t *testing.T) {
	dir := t.TempDir()
	firstPath := filepath.Join(dir, "foo bar")
	secondPath := filepath.Join(dir, "foo@bar")
	for _, path := range []string{firstPath, secondPath} {
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := SlugFromPath(secondPath), SlugFromPath(firstPath); got != want {
		t.Fatalf("test paths must collide: second slug %q, first slug %q", got, want)
	}
	configPath := writeTempConfig(t, `
collections:
  - name: `+SlugFromPath(firstPath)+`
    path: `+firstPath+`
`)

	err := AddCollection(configPath, Collection{Path: secondPath})
	if err == nil {
		t.Fatal("expected slug collision error")
	}
	if !strings.Contains(err.Error(), "collides with existing path") {
		t.Fatalf("expected collision error, got %v", err)
	}
	cfg, loadErr := Load(configPath)
	if loadErr != nil {
		t.Fatalf("Load failed: %v", loadErr)
	}
	if got, want := len(cfg.Collections), 1; got != want {
		t.Fatalf("collection count = %d, want %d", got, want)
	}
	if got := cfg.Collections[0].Path; got != firstPath {
		t.Fatalf("existing collection path was overwritten: got %q, want %q", got, firstPath)
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

func TestCanonicalPath(t *testing.T) {
	dir := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatalf("restoring working directory: %v", err)
		}
	}()

	got, err := CanonicalPath("missing/../file.txt")
	if err != nil {
		t.Fatalf("CanonicalPath returned error: %v", err)
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(realDir, "file.txt")
	if got != want {
		t.Fatalf("CanonicalPath relative path = %q, want %q", got, want)
	}
}

func TestCanonicalPathExpandsHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	got, err := CanonicalPath("~/qi-canonical-test-missing")
	if err != nil {
		t.Fatalf("CanonicalPath returned error: %v", err)
	}
	want := filepath.Join(home, "qi-canonical-test-missing")
	if got != want {
		t.Fatalf("CanonicalPath home path = %q, want %q", got, want)
	}
}

func TestEmbeddingFingerprintChangesWithProviderOrEndpoint(t *testing.T) {
	base := (&EmbeddingProviderConfig{Name: "local", BaseURL: "HTTP://Example.COM/v1/", Model: "m", Dimension: 4}).Fingerprint()
	if base == (&EmbeddingProviderConfig{Name: "remote", BaseURL: "HTTP://Example.COM/v1/", Model: "m", Dimension: 4}).Fingerprint() {
		t.Fatal("provider switch must change fingerprint")
	}
	if base == (&EmbeddingProviderConfig{Name: "local", BaseURL: "https://example.com/v1", Model: "m", Dimension: 4}).Fingerprint() {
		t.Fatal("endpoint switch must change fingerprint")
	}
	if base != (&EmbeddingProviderConfig{Name: "local", BaseURL: "http://example.com/v1", Model: "m", Dimension: 4}).Fingerprint() {
		t.Fatal("cosmetic endpoint casing/trailing slash should be stable")
	}
}

func TestLoadRejectsNonPositiveEmbeddingDimension(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("providers:\n  embedding:\n    base_url: http://localhost:1\n    model: m\n    dimension: 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected non-positive embedding dimension to be rejected")
	}
}

func TestCanonicalPathResolvesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	got, err := CanonicalPath(link)
	if err != nil {
		t.Fatalf("CanonicalPath returned error: %v", err)
	}
	realTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if got != realTarget {
		t.Fatalf("CanonicalPath symlink = %q, want %q", got, realTarget)
	}
}

func TestSlugFromPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "mac user project path",
			path: "/Users/alice/Projects/tools/qi",
			want: "qi",
		},
		{
			name: "linux user project path",
			path: "/home/alice/Projects/tools/qi",
			want: "qi",
		},
		{
			name: "spaces and punctuation",
			path: "/tmp/My Notes/docs.v1",
			want: "docs-v1",
		},
		{
			name: "root fallback",
			path: "/",
			want: "collection",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SlugFromPath(tt.path); got != tt.want {
				t.Fatalf("SlugFromPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestAssignCollectionNames(t *testing.T) {
	tests := []struct {
		name  string
		paths []string
		want  []string
	}{
		{
			name:  "distinct basenames stay short",
			paths: []string{"/Users/alice/Projects/tools/qi", "/Users/alice/Documents/health"},
			want:  []string{"qi", "health"},
		},
		{
			name:  "collision lengthens only the collided",
			paths: []string{"/Users/alice/work/notes", "/Users/alice/personal/notes", "/Users/alice/qi"},
			want:  []string{"work-notes", "personal-notes", "qi"},
		},
		{
			name:  "lengthens until distinct",
			paths: []string{"/Users/alice/a/x/notes", "/Users/alice/b/x/notes"},
			want:  []string{"a-x-notes", "b-x-notes"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AssignCollectionNames(tt.paths)
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("names[%d] = %q, want %q (all: %v)", i, got[i], tt.want[i], got)
				}
			}
		})
	}
}

// A fixed depth cap used to stop lengthening before deep paths diverged,
// collapsing two distinct collections onto one name.
func TestAssignCollectionNamesDisambiguatesDeepPaths(t *testing.T) {
	got := AssignCollectionNames([]string{
		"/Users/alice/a/b/c/d/e/f/g/h/notes",
		"/Users/alice/z/b/c/d/e/f/g/h/notes",
	})
	if got[0] == got[1] {
		t.Fatalf("distinct paths collapsed to %q", got[0])
	}
}

// Termination must not depend on a round cap: the same path twice shares every
// segment and can never be disambiguated.
func TestAssignCollectionNamesTerminatesOnIdenticalPaths(t *testing.T) {
	got := AssignCollectionNames([]string{"/Users/alice/notes", "/Users/alice/notes"})
	if got[0] != got[1] {
		t.Errorf("identical paths got different names: %v", got)
	}
}

// A collision-lengthened name must still resolve on removal: the config stores
// the short legacy name, but every command refers to the assigned one.
func TestRemoveCollectionResolvesCollisionLengthenedName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("collections:\n  - name: notes\n    path: /work/notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AddCollection(path, Collection{Path: "/personal/notes"}); err != nil {
		t.Fatalf("AddCollection: %v", err)
	}
	if err := RemoveCollection(path, "work-notes"); err != nil {
		t.Fatalf("RemoveCollection(work-notes): %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Collections) != 1 || cfg.Collections[0].Path != "/personal/notes" {
		t.Fatalf("collections = %+v, want only /personal/notes", cfg.Collections)
	}
}
