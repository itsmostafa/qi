package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/itsmostafa/qi/internal/app"
	"github.com/itsmostafa/qi/internal/db"
)

func TestDoctorSkipsEmbeddingCompletenessWhenNotConfigured(t *testing.T) {
	cfgPath := makeDoctorTestConfig(t, false)
	withIndexTestConfig(t, cfgPath)

	var runErr error
	output := captureIndexTestOutput(t, func() { runErr = doctorCmd.RunE(doctorCmd, nil) })
	if runErr != nil {
		t.Fatalf("lexical-only doctor should pass: %v\n%s", runErr, output)
	}
	if strings.Contains(output, "embeddings:") {
		t.Fatalf("lexical-only doctor reported embedding completeness:\n%s", output)
	}
	if !strings.Contains(output, "All checks passed.") {
		t.Fatalf("expected green lexical-only result:\n%s", output)
	}
}

func TestDoctorConfiguredMissingEmbeddingsFails(t *testing.T) {
	cfgPath := makeDoctorTestConfig(t, true)
	withIndexTestConfig(t, cfgPath)

	var runErr error
	output := captureIndexTestOutput(t, func() { runErr = doctorCmd.RunE(doctorCmd, nil) })
	if runErr == nil {
		t.Fatalf("configured missing embeddings must fail doctor:\n%s", output)
	}
	if !strings.Contains(output, "WARN  embeddings: 0 current / 1 missing") {
		t.Fatalf("missing embedding health warning not shown:\n%s", output)
	}
	if strings.Contains(output, "All checks passed.") {
		t.Fatalf("unhealthy doctor printed green result:\n%s", output)
	}
}

func TestDoctorIndexRepairBecomesHealthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[1,0],"index":0}]}`))
	}))
	defer server.Close()
	dir := t.TempDir()
	docs := filepath.Join(dir, "docs")
	if err := os.Mkdir(docs, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "a.md"), []byte("# A\nContent."), 0o640); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeIndexTestConfig(t, fmt.Sprintf(`database_path: %s
collections:
  - path: %s
providers:
  embedding:
    name: test
    base_url: %s
    model: m
    dimension: 2
search:
  chunk_size: 256
`, filepath.Join(dir, "qi.db"), docs, server.URL))
	withIndexTestConfig(t, cfgPath)
	ctx := context.Background()
	a, err := app.New(ctx, cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Indexer.Index(ctx, a.Config.Collections[0]); err != nil {
		t.Fatal(err)
	}
	a.Close()

	var doctorErr error
	before := captureIndexTestOutput(t, func() { doctorErr = doctorCmd.RunE(doctorCmd, nil) })
	if doctorErr == nil || !strings.Contains(before, "1 missing") {
		t.Fatalf("doctor did not warn before repair: err=%v\n%s", doctorErr, before)
	}

	a, err = app.New(ctx, cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	var indexErr error
	captureIndexTestOutput(t, func() { indexErr = runIndex(ctx, a, a.Config.Collections) })
	a.Close()
	if indexErr != nil {
		t.Fatalf("qi index repair failed: %v", indexErr)
	}

	after := captureIndexTestOutput(t, func() { doctorErr = doctorCmd.RunE(doctorCmd, nil) })
	if doctorErr != nil || !strings.Contains(after, "1 current / 0 missing / 0 stale / 0 orphaned") || !strings.Contains(after, "All checks passed.") {
		t.Fatalf("doctor not healthy after repair: err=%v\n%s", doctorErr, after)
	}
}

func TestStatsSaysNotConfiguredForLexicalOnlyDatabase(t *testing.T) {
	cfgPath := makeDoctorTestConfig(t, false)
	withIndexTestConfig(t, cfgPath)

	var runErr error
	output := captureIndexTestOutput(t, func() { runErr = statsCmd.RunE(statsCmd, nil) })
	if runErr != nil {
		t.Fatal(runErr)
	}
	if !strings.Contains(output, "Embeddings: not configured") || strings.Contains(output, "missing") {
		t.Fatalf("ambiguous lexical-only embedding stats:\n%s", output)
	}
}

func makeDoctorTestConfig(t *testing.T, embedding bool) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "qi.db")
	database, err := db.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO content(hash, body) VALUES ('h', 'body');
		INSERT INTO documents(collection, path, content_hash) VALUES ('test', 'a.md', 'h');
		INSERT INTO chunks(content_hash, doc_id, seq, text, content_length) VALUES ('h', 1, 0, 'body', 4);
	`); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	providers := ""
	if embedding {
		providers = "providers:\n  embedding:\n    name: test\n    base_url: http://localhost:1\n    model: m\n    dimension: 2\n"
	}
	return writeIndexTestConfig(t, fmt.Sprintf("database_path: %s\n%s", dbPath, providers))
}
