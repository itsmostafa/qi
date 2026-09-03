package cmd

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/itsmostafa/qi/internal/app"
	"github.com/itsmostafa/qi/internal/config"
	"github.com/itsmostafa/qi/internal/db"
	"github.com/itsmostafa/qi/internal/indexer"
)

func TestIndexCommandDoesNotAcceptNameFlag(t *testing.T) {
	if flag := indexCmd.Flags().Lookup("name"); flag != nil {
		t.Fatal("index command should not expose --name")
	}
}

func TestFindCollectionByPathResolvesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	collections := []config.Collection{{Name: "docs", Path: link}}
	if got := findCollectionByPath(collections, target); got == nil {
		t.Fatal("expected symlinked collection path to match target path")
	}
}

func TestAutoCollectionNormalizesLegacyNameOncePerApp(t *testing.T) {
	collectionPath := t.TempDir()
	cfgPath := writeIndexTestConfig(t, `
collections:
  - name: legacy
    path: `+collectionPath+`
`)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	a := &app.App{Config: cfg}

	withIndexTestConfig(t, cfgPath)

	first := captureIndexTestOutput(t, func() {
		if _, err := autoCollection(a, collectionPath); err != nil {
			t.Fatalf("first autoCollection failed: %v", err)
		}
	})
	if !strings.Contains(first, "Updated collection") {
		t.Fatalf("expected first call to print update, got %q", first)
	}

	second := captureIndexTestOutput(t, func() {
		if _, err := autoCollection(a, collectionPath); err != nil {
			t.Fatalf("second autoCollection failed: %v", err)
		}
	})
	if strings.Contains(second, "Updated collection") {
		t.Fatalf("expected second call not to print update, got %q", second)
	}
	if a.Config.Collections[0].OriginalName != "" {
		t.Fatalf("expected original name to be cleared, got %q", a.Config.Collections[0].OriginalName)
	}
}

func TestAutoCollectionSavesNewCollectionOncePerApp(t *testing.T) {
	collectionPath := t.TempDir()
	cfgPath := writeIndexTestConfig(t, "collections: []\n")
	a := &app.App{Config: &config.Config{}}

	withIndexTestConfig(t, cfgPath)

	first := captureIndexTestOutput(t, func() {
		if _, err := autoCollection(a, collectionPath); err != nil {
			t.Fatalf("first autoCollection failed: %v", err)
		}
	})
	if !strings.Contains(first, "Saved collection") {
		t.Fatalf("expected first call to print save, got %q", first)
	}

	second := captureIndexTestOutput(t, func() {
		if _, err := autoCollection(a, collectionPath); err != nil {
			t.Fatalf("second autoCollection failed: %v", err)
		}
	})
	if strings.Contains(second, "Saved collection") {
		t.Fatalf("expected second call not to print save, got %q", second)
	}
	if len(a.Config.Collections) != 1 {
		t.Fatalf("expected one in-memory collection, got %d", len(a.Config.Collections))
	}
}

func TestRunIndexEmbedsWhenProviderConfigured(t *testing.T) {
	ctx := context.Background()
	collectionPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(collectionPath, "doc.md"), []byte("# Doc\nGo is useful."), 0o640); err != nil {
		t.Fatal(err)
	}

	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "qi.db"))
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	a := &app.App{
		Indexer:  indexer.New(database, 256),
		Embedder: indexer.NewEmbedder(database, testEmbeddingProvider{}, "test", "test-fingerprint"),
	}
	col := config.Collection{Name: "test", Path: collectionPath, Extensions: []string{".md"}}

	var runErr error
	output := captureIndexTestOutput(t, func() {
		runErr = runIndex(ctx, a, []config.Collection{col})
	})
	if runErr != nil {
		t.Fatalf("runIndex failed: %v", runErr)
	}
	if !strings.Contains(output, "embedding chunks") {
		t.Fatalf("expected embedding status in output, got %q", output)
	}

	var chunkCount, embeddingCount int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM chunks`).Scan(&chunkCount); err != nil {
		t.Fatalf("counting chunks: %v", err)
	}
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM embeddings`).Scan(&embeddingCount); err != nil {
		t.Fatalf("counting embeddings: %v", err)
	}
	if chunkCount == 0 {
		t.Fatal("expected indexed chunks")
	}
	if embeddingCount != chunkCount {
		t.Fatalf("expected embeddings for every chunk, got embeddings=%d chunks=%d", embeddingCount, chunkCount)
	}
}

func TestRunIndexPropagatesAllCollectionFailures(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "qi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	a := &app.App{Indexer: indexer.New(database, 256)}
	cols := []config.Collection{
		{Name: "missing-one", Path: filepath.Join(t.TempDir(), "gone-one")},
		{Name: "missing-two", Path: filepath.Join(t.TempDir(), "gone-two")},
	}
	var runErr error
	captureIndexTestOutput(t, func() { runErr = runIndex(ctx, a, cols) })
	if runErr == nil {
		t.Fatal("expected collection failures to propagate")
	}
	if !strings.Contains(runErr.Error(), "missing-one") || !strings.Contains(runErr.Error(), "missing-two") {
		t.Fatalf("expected aggregate error to include both collections, got %v", runErr)
	}
}

func writeIndexTestConfig(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0o640); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	return cfgPath
}

func withIndexTestConfig(t *testing.T, path string) {
	t.Helper()

	oldCfgFile := cfgFile
	cfgFile = path
	t.Cleanup(func() { cfgFile = oldCfgFile })
}

func captureIndexTestOutput(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("opening stdout pipe: %v", err)
	}
	os.Stdout = writer
	defer func() { os.Stdout = oldStdout }()

	fn()

	if err := writer.Close(); err != nil {
		t.Fatalf("closing stdout pipe: %v", err)
	}
	out, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("reading stdout pipe: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("closing stdout reader: %v", err)
	}
	return string(out)
}

type testEmbeddingProvider struct{}

func (testEmbeddingProvider) Embed(_ context.Context, texts []string) ([][]float32, error) {
	embeddings := make([][]float32, len(texts))
	for i := range texts {
		embeddings[i] = []float32{1, 0, 0, 0}
	}
	return embeddings, nil
}

func (testEmbeddingProvider) ModelName() string { return "test-model" }

func (testEmbeddingProvider) Dimension() int { return 4 }
