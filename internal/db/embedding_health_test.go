package db

import (
	"context"
	"math"
	"testing"
)

func TestEmbeddingHealthClassifiesAllStates(t *testing.T) {
	ctx := context.Background()
	database := openMemoryDB(t)
	defer database.Close()

	for i := 0; i < 7; i++ {
		insertHealthChunk(t, database, i)
	}
	// chunk 1 current, chunk 2 stale, chunk 3 missing, chunk 4 one-sided/orphaned.
	if err := database.UpsertEmbedding(ctx, 1, []float32{1}, "p", "m", 1, "current"); err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertEmbedding(ctx, 2, []float32{1}, "p", "m", 1, "old"); err != nil {
		t.Fatal(err)
	}
	if err := database.InsertEmbedding(ctx, 4, []float32{1}); err != nil {
		t.Fatal(err)
	}
	for id, vector := range map[int][]float32{
		5: {0},
		6: {float32(math.NaN())},
		7: {float32(math.Inf(1))},
	} {
		if _, err := database.ExecContext(ctx, `INSERT INTO chunk_vectors(chunk_id, vector) VALUES (?, ?)`, id, serializeFloat32(vector)); err != nil {
			t.Fatal(err)
		}
		if _, err := database.ExecContext(ctx, `INSERT INTO embeddings(chunk_id, provider, model, dimension, fingerprint) VALUES (?, 'p', 'm', 1, 'current')`, id); err != nil {
			t.Fatal(err)
		}
	}

	health, err := database.EmbeddingHealth(ctx, "current", 1, "test")
	if err != nil {
		t.Fatal(err)
	}
	if health != (EmbeddingHealth{Current: 1, Missing: 1, Stale: 1, Orphaned: 4}) {
		t.Fatalf("unexpected health: %+v", health)
	}
}

func openMemoryDB(t *testing.T) *DB {
	t.Helper()
	database, err := Open(context.Background(), t.TempDir()+"/qi.db")
	if err != nil {
		t.Fatal(err)
	}
	return database
}

func insertHealthChunk(t *testing.T, database *DB, i int) {
	t.Helper()
	ctx := context.Background()
	hash := string(rune('a' + i))
	if _, err := database.ExecContext(ctx, `INSERT INTO content(hash, body) VALUES (?, 'x')`, hash); err != nil {
		t.Fatal(err)
	}
	result, err := database.ExecContext(ctx, `INSERT INTO documents(collection, path, content_hash) VALUES ('test', ?, ?)`, hash, hash)
	if err != nil {
		t.Fatal(err)
	}
	docID, _ := result.LastInsertId()
	if _, err := database.ExecContext(ctx, `INSERT INTO chunks(id, content_hash, doc_id, seq, text, content_length) VALUES (?, ?, ?, 0, 'x', 1)`, i+1, hash, docID); err != nil {
		t.Fatal(err)
	}
}
