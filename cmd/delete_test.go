package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/itsmostafa/qi/internal/config"
	"github.com/itsmostafa/qi/internal/db"
)

func TestDeleteCommandDeletesIndexedOnlyCollection(t *testing.T) {
	cfgPath, dbPath := writeDeleteTestConfig(t, "")
	insertDeleteTestCollection(t, dbPath, "indexed-only")
	insertDeleteTestCollection(t, dbPath, "keep")

	runDeleteCommand(t, cfgPath, "indexed-only")

	database := openDeleteTestDB(t, dbPath)
	defer database.Close()

	assertDeleteTestCount(t, database, `SELECT COUNT(*) FROM documents WHERE collection = 'indexed-only'`, 0)
	assertDeleteTestCount(t, database, `SELECT COUNT(*) FROM index_runs WHERE collection = 'indexed-only'`, 0)
	assertDeleteTestCount(t, database, `SELECT COUNT(*) FROM documents WHERE collection = 'keep'`, 1)
	assertDeleteTestCount(t, database, `SELECT COUNT(*) FROM chunks`, 1)
	assertDeleteTestCount(t, database, `SELECT COUNT(*) FROM embeddings`, 1)
	assertDeleteTestCount(t, database, `SELECT COUNT(*) FROM chunk_vectors`, 1)
}

func TestDeleteCommandDeletesNamedCollectionAndConfigEntry(t *testing.T) {
	collectionPath := t.TempDir()
	cfgPath, dbPath := writeDeleteTestConfig(t, fmt.Sprintf(`
collections:
  - name: named
    path: %s
`, collectionPath))
	insertDeleteTestCollection(t, dbPath, "named")

	runDeleteCommand(t, cfgPath, "named")

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("loading config after delete: %v", err)
	}
	for _, col := range cfg.Collections {
		if col.Name == "named" {
			t.Fatalf("named collection still present in config: %+v", cfg.Collections)
		}
	}

	database := openDeleteTestDB(t, dbPath)
	defer database.Close()
	assertDeleteTestCount(t, database, `SELECT COUNT(*) FROM documents WHERE collection = 'named'`, 0)
	assertDeleteTestCount(t, database, `SELECT COUNT(*) FROM chunks`, 0)
	assertDeleteTestCount(t, database, `SELECT COUNT(*) FROM embeddings`, 0)
	assertDeleteTestCount(t, database, `SELECT COUNT(*) FROM chunk_vectors`, 0)
	assertDeleteTestCount(t, database, `SELECT COUNT(*) FROM index_runs WHERE collection = 'named'`, 0)
}

func TestDeleteCommandNotFound(t *testing.T) {
	cfgPath, _ := writeDeleteTestConfig(t, "")

	oldCfgFile := cfgFile
	cfgFile = cfgPath
	t.Cleanup(func() { cfgFile = oldCfgFile })

	err := deleteCmd.RunE(deleteCmd, []string{"missing"})
	if err == nil {
		t.Fatal("expected error for missing collection")
	}
	if got, want := err.Error(), `collection "missing" not found`; got != want {
		t.Fatalf("unexpected error:\ngot:  %s\nwant: %s", got, want)
	}
}

func writeDeleteTestConfig(t *testing.T, extra string) (string, string) {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "qi.db")
	cfgPath := filepath.Join(dir, "config.yaml")
	content := fmt.Sprintf("database_path: %s\n%s", dbPath, extra)
	if err := os.WriteFile(cfgPath, []byte(content), 0o640); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	return cfgPath, dbPath
}

func runDeleteCommand(t *testing.T, cfgPath, name string) {
	t.Helper()

	oldCfgFile := cfgFile
	cfgFile = cfgPath
	t.Cleanup(func() { cfgFile = oldCfgFile })

	if err := deleteCmd.RunE(deleteCmd, []string{name}); err != nil {
		t.Fatalf("delete command failed: %v", err)
	}
}

func insertDeleteTestCollection(t *testing.T, dbPath, collection string) {
	t.Helper()

	database := openDeleteTestDB(t, dbPath)
	defer database.Close()

	hash := fmt.Sprintf("%064x", len(collection)+1)
	if _, err := database.ExecContext(context.Background(),
		`INSERT OR IGNORE INTO content(hash, body) VALUES (?, ?)`,
		hash, []byte("body")); err != nil {
		t.Fatalf("inserting content: %v", err)
	}

	result, err := database.ExecContext(context.Background(), `
		INSERT INTO documents(collection, path, title, content_hash)
		VALUES (?, ?, ?, ?)
	`, collection, "doc.md", "Doc", hash)
	if err != nil {
		t.Fatalf("inserting document: %v", err)
	}
	docID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("reading document id: %v", err)
	}

	result, err = database.ExecContext(context.Background(), `
		INSERT INTO chunks(content_hash, doc_id, seq, text, content_length)
		VALUES (?, ?, 0, ?, ?)
	`, hash, docID, "chunk text", len("chunk text"))
	if err != nil {
		t.Fatalf("inserting chunk: %v", err)
	}
	chunkID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("reading chunk id: %v", err)
	}

	if _, err := database.ExecContext(context.Background(),
		`INSERT INTO chunk_vectors(chunk_id, vector) VALUES (?, ?)`,
		chunkID, []byte{0, 0, 0, 0}); err != nil {
		t.Fatalf("inserting chunk vector: %v", err)
	}
	if _, err := database.ExecContext(context.Background(), `
		INSERT INTO embeddings(chunk_id, provider, model, dimension)
		VALUES (?, 'test', 'test-model', 1)
	`, chunkID); err != nil {
		t.Fatalf("inserting embedding: %v", err)
	}
	if _, err := database.ExecContext(context.Background(),
		`INSERT INTO index_runs(collection, finished_at) VALUES (?, datetime('now'))`,
		collection); err != nil {
		t.Fatalf("inserting index run: %v", err)
	}
}

func openDeleteTestDB(t *testing.T, dbPath string) *db.DB {
	t.Helper()

	database, err := db.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	return database
}

func assertDeleteTestCount(t *testing.T, database *db.DB, query string, want int) {
	t.Helper()

	var got int
	if err := database.QueryRowContext(context.Background(), query).Scan(&got); err != nil {
		t.Fatalf("querying count: %v", err)
	}
	if got != want {
		t.Fatalf("unexpected count for %q: got %d, want %d", query, got, want)
	}
}
