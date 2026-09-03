package indexer

import (
	"context"
	"encoding/binary"
	"math"
	"strings"
	"testing"

	"github.com/itsmostafa/qi/internal/config"
)

// fakeEmbeddingProvider returns deterministic vectors and counts how many
// times Embed is invoked, so tests can assert re-embedding did or didn't
// happen without depending on wall-clock timing.
type fakeEmbeddingProvider struct {
	model     string
	dimension int
	calls     int
	texts     int
}

func (p *fakeEmbeddingProvider) Embed(_ context.Context, texts []string) ([][]float32, error) {
	p.calls++
	p.texts += len(texts)
	out := make([][]float32, len(texts))
	for i := range texts {
		vec := make([]float32, p.dimension)
		vec[0] = 1
		out[i] = vec
	}
	return out, nil
}

func (p *fakeEmbeddingProvider) ModelName() string { return p.model }
func (p *fakeEmbeddingProvider) Dimension() int    { return p.dimension }

func TestEmbedder_EmbedsPendingChunksWithFingerprint(t *testing.T) {
	database := openTestDB(t)
	idx := New(database, 256)
	col := makeTestCollection(t, map[string]string{"a.md": "# A\nSome content about Go."})
	if _, err := idx.Index(context.Background(), col); err != nil {
		t.Fatal(err)
	}

	provider := &fakeEmbeddingProvider{model: "model-a", dimension: 4}
	fp := (&config.EmbeddingProviderConfig{Model: "model-a", Dimension: 4}).Fingerprint()
	emb := NewEmbedder(database, provider, "test-provider", fp)
	if err := emb.EmbedCollection(context.Background(), "test"); err != nil {
		t.Fatalf("EmbedCollection failed: %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("expected 1 embed call, got %d", provider.calls)
	}

	var storedFingerprint, storedProvider, storedModel string
	var storedDim int
	if err := database.QueryRowContext(context.Background(),
		`SELECT fingerprint, provider, model, dimension FROM embeddings LIMIT 1`).
		Scan(&storedFingerprint, &storedProvider, &storedModel, &storedDim); err != nil {
		t.Fatalf("querying stored embedding: %v", err)
	}
	if storedFingerprint != fp {
		t.Errorf("expected stored fingerprint %q, got %q", fp, storedFingerprint)
	}
	if storedProvider != "test-provider" {
		t.Errorf("expected real provider tag %q, got %q (must not be hardcoded 'http')", "test-provider", storedProvider)
	}
	if storedModel != "model-a" || storedDim != 4 {
		t.Errorf("expected model=model-a dimension=4, got model=%q dimension=%d", storedModel, storedDim)
	}
}

func TestEmbedder_NoOpWhenFingerprintUnchanged(t *testing.T) {
	database := openTestDB(t)
	idx := New(database, 256)
	col := makeTestCollection(t, map[string]string{"a.md": "# A\nSome content about Go."})
	if _, err := idx.Index(context.Background(), col); err != nil {
		t.Fatal(err)
	}

	provider := &fakeEmbeddingProvider{model: "model-a", dimension: 4}
	fp := (&config.EmbeddingProviderConfig{Model: "model-a", Dimension: 4}).Fingerprint()
	emb := NewEmbedder(database, provider, "test-provider", fp)
	if err := emb.EmbedCollection(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 1 {
		t.Fatalf("expected 1 embed call after first run, got %d", provider.calls)
	}

	// Re-run with the same fingerprint: nothing should be re-embedded.
	if err := emb.EmbedCollection(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 1 {
		t.Fatalf("expected no additional embed calls when the fingerprint is unchanged, got %d total", provider.calls)
	}
}

func TestEmbedder_ReEmbedsAfterFingerprintChange(t *testing.T) {
	database := openTestDB(t)
	idx := New(database, 256)
	col := makeTestCollection(t, map[string]string{"a.md": "# A\nSome content about Go."})
	if _, err := idx.Index(context.Background(), col); err != nil {
		t.Fatal(err)
	}

	fpA := (&config.EmbeddingProviderConfig{Model: "model-a", Dimension: 4}).Fingerprint()
	providerA := &fakeEmbeddingProvider{model: "model-a", dimension: 4}
	embA := NewEmbedder(database, providerA, "test-provider", fpA)
	if err := embA.EmbedCollection(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}

	var chunkID int64
	if err := database.QueryRowContext(context.Background(), `SELECT id FROM chunks LIMIT 1`).Scan(&chunkID); err != nil {
		t.Fatal(err)
	}

	// Switch to a different model/dimension — a distinct provider config.
	fpB := (&config.EmbeddingProviderConfig{Model: "model-b", Dimension: 8}).Fingerprint()
	if fpA == fpB {
		t.Fatal("expected different fingerprints for different models/dimensions")
	}
	providerB := &fakeEmbeddingProvider{model: "model-b", dimension: 8}
	embB := NewEmbedder(database, providerB, "test-provider", fpB)
	if err := embB.EmbedCollection(context.Background(), "test"); err != nil {
		t.Fatalf("re-embed after fingerprint change failed: %v", err)
	}
	if providerB.calls != 1 {
		t.Fatalf("expected the stale-fingerprint chunk to be re-embedded, got %d calls", providerB.calls)
	}

	var storedFingerprint, storedModel string
	var storedDim int
	if err := database.QueryRowContext(context.Background(),
		`SELECT fingerprint, model, dimension FROM embeddings WHERE chunk_id = ?`, chunkID).
		Scan(&storedFingerprint, &storedModel, &storedDim); err != nil {
		t.Fatalf("querying updated embedding: %v", err)
	}
	if storedFingerprint != fpB || storedModel != "model-b" || storedDim != 8 {
		t.Errorf("expected chunk to now carry fingerprint=%q model=model-b dimension=8, got fingerprint=%q model=%q dimension=%d",
			fpB, storedFingerprint, storedModel, storedDim)
	}

	var vecLen int
	if err := database.QueryRowContext(context.Background(),
		`SELECT length(vector) FROM chunk_vectors WHERE chunk_id = ?`, chunkID).Scan(&vecLen); err != nil {
		t.Fatalf("querying updated vector: %v", err)
	}
	if vecLen != 8*4 {
		t.Errorf("expected the stored vector to be re-written at the new dimension (8 float32s = 32 bytes), got %d bytes", vecLen)
	}
}

func TestEmbedderRepairsEveryInvalidVectorState(t *testing.T) {
	ctx := context.Background()
	database := openTestDB(t)
	idx := New(database, 256)
	files := map[string]string{}
	for _, name := range []string{"metadata-only.md", "vector-only.md", "malformed.md", "wrong-size.md", "wrong-dimension.md", "zero.md", "nan.md", "inf.md"} {
		files[name] = "# " + name + "\nContent."
	}
	col := makeTestCollection(t, files)
	if _, err := idx.Index(ctx, col); err != nil {
		t.Fatal(err)
	}
	provider := &fakeEmbeddingProvider{model: "model", dimension: 2}
	embedder := NewEmbedder(database, provider, "provider", "current")
	if err := embedder.EmbedCollection(ctx, "test"); err != nil {
		t.Fatal(err)
	}
	provider.calls, provider.texts = 0, 0

	ids := map[string]int64{}
	rows, err := database.QueryContext(ctx, `SELECT d.path, c.id FROM chunks c JOIN documents d ON d.id=c.doc_id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var path string
		var id int64
		if err := rows.Scan(&path, &id); err != nil {
			t.Fatal(err)
		}
		ids[path] = id
	}
	rows.Close()
	if _, err := database.ExecContext(ctx, `DELETE FROM chunk_vectors WHERE chunk_id=?`, ids["metadata-only.md"]); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `DELETE FROM embeddings WHERE chunk_id=?`, ids["vector-only.md"]); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE embeddings SET dimension=1 WHERE chunk_id=?`, ids["wrong-dimension.md"]); err != nil {
		t.Fatal(err)
	}
	corrupt := map[string][]byte{
		"malformed.md":  {1, 2, 3},
		"wrong-size.md": vectorBlob(1),
		"zero.md":       vectorBlob(0, 0),
		"nan.md":        vectorBlob(float32(math.NaN()), 1),
		"inf.md":        vectorBlob(float32(math.Inf(1)), 1),
	}
	for path, blob := range corrupt {
		if _, err := database.ExecContext(ctx, `UPDATE chunk_vectors SET vector=? WHERE chunk_id=?`, blob, ids[path]); err != nil {
			t.Fatal(err)
		}
	}

	health, err := database.EmbeddingHealth(ctx, "current", 2, "test")
	if err != nil {
		t.Fatal(err)
	}
	if health.Current != 0 || health.Orphaned != 8 {
		t.Fatalf("invalid states classified as healthy: %+v", health)
	}
	if err := embedder.EmbedCollection(ctx, "test"); err != nil {
		t.Fatalf("repair failed: %v", err)
	}
	if provider.calls != 1 || provider.texts != 8 {
		t.Fatalf("expected all 8 invalid chunks to be re-embedded, calls=%d texts=%d", provider.calls, provider.texts)
	}
	health, err = database.EmbeddingHealth(ctx, "current", 2, "test")
	if err != nil {
		t.Fatal(err)
	}
	if health.Current != 8 || health.Missing+health.Stale+health.Orphaned != 0 {
		t.Fatalf("repair did not produce fully current embeddings: %+v", health)
	}
}

func vectorBlob(values ...float32) []byte {
	blob := make([]byte, len(values)*4)
	for i, value := range values {
		binary.LittleEndian.PutUint32(blob[i*4:], math.Float32bits(value))
	}
	return blob
}

// Guard against silently swallowing a provider that returns the wrong
// number of vectors (a truncated/malformed batch response).
func TestEmbedder_ErrorsOnMismatchedResponseCount(t *testing.T) {
	database := openTestDB(t)
	idx := New(database, 256)
	col := makeTestCollection(t, map[string]string{
		"a.md": "# A\nContent one.",
		"b.md": "# B\nContent two.",
	})
	if _, err := idx.Index(context.Background(), col); err != nil {
		t.Fatal(err)
	}

	emb := NewEmbedder(database, shortProvider{}, "test-provider", "fp")
	err := emb.EmbedCollection(context.Background(), "test")
	if err == nil {
		t.Fatal("expected an error when the provider returns fewer vectors than requested")
	}
}

func TestEmbedderReturnsAggregatePersistenceErrors(t *testing.T) {
	database := openTestDB(t)
	idx := New(database, 256)
	col := makeTestCollection(t, map[string]string{
		"a.md": "# A\nContent one.",
		"b.md": "# B\nContent two.",
	})
	if _, err := idx.Index(context.Background(), col); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(context.Background(), `
		CREATE TRIGGER fail_one_vector BEFORE INSERT ON chunk_vectors
		WHEN new.chunk_id = (SELECT MIN(id) FROM chunks)
		BEGIN SELECT RAISE(FAIL, 'forced vector failure'); END
	`); err != nil {
		t.Fatal(err)
	}
	err := NewEmbedder(database, mixedValidityProvider{}, "p", "fp").EmbedCollection(context.Background(), "test")
	if err == nil || !strings.Contains(err.Error(), "forced vector failure") {
		t.Fatalf("expected failed vector write to be returned, got %v", err)
	}
	var stored int
	if scanErr := database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM embeddings`).Scan(&stored); scanErr != nil {
		t.Fatal(scanErr)
	}
	if stored != 1 {
		t.Fatalf("valid writes should persist while invalid writes are aggregated; got %d", stored)
	}
}

type mixedValidityProvider struct{}

func (mixedValidityProvider) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = []float32{1, 0}
	}
	return out, nil
}
func (mixedValidityProvider) ModelName() string { return "mixed" }
func (mixedValidityProvider) Dimension() int    { return 2 }

type shortProvider struct{}

func (shortProvider) Embed(_ context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	return [][]float32{{1, 2, 3, 4}}, nil // always returns exactly one, regardless of input size
}
func (shortProvider) ModelName() string { return "short" }
func (shortProvider) Dimension() int    { return 4 }
