package search

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/itsmostafa/qi/internal/config"
	"github.com/itsmostafa/qi/internal/db"
	"github.com/itsmostafa/qi/internal/providers"
)

// seedCandidateCorpus stores three documents whose vectors rank them in the
// reverse of their lexical relevance: rust.md, which shares no term with the
// queries below, holds the vector nearest the query embedding [1, 0].
func seedCandidateCorpus(t *testing.T) (*db.DB, *Hybrid) {
	t.Helper()
	ctx := context.Background()
	database := openTestDB(t)
	if _, err := database.ExecContext(ctx, `
		INSERT INTO content(hash, body) VALUES ('h1', 'b1'), ('h2', 'b2'), ('h3', 'b3');
		INSERT INTO documents(id, collection, path, title, content_hash) VALUES
			(1, 'test', 'go.md', 'Go', 'h1'),
			(2, 'test', 'python.md', 'Python', 'h2'),
			(3, 'test', 'rust.md', 'Rust', 'h3');
		INSERT INTO chunks(id, content_hash, doc_id, seq, text, content_length, start_line, end_line) VALUES
			(1, 'h1', 1, 0, 'Go is a compiled programming language.', 38, 1, 1),
			(2, 'h2', 2, 0, 'Python is an interpreted language.', 34, 1, 1),
			(3, 'h3', 3, 0, 'Ownership and borrowing keep memory safe.', 41, 1, 1);
	`); err != nil {
		t.Fatal(err)
	}
	for id, v := range map[int64][]float32{1: {0, 1}, 2: {0.5, 0.5}, 3: {1, 0}} {
		if err := database.UpsertEmbedding(ctx, id, v, "test", "model", 2, "fp"); err != nil {
			t.Fatal(err)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[1,0]}]}`))
	}))
	t.Cleanup(srv.Close)
	embedder := providers.NewEmbedding(&config.EmbeddingProviderConfig{BaseURL: srv.URL, Model: "model", Dimension: 2})
	return database, NewHybrid(NewBM25(database), NewVectorSearch(database, "fp"), embedder, config.SearchConfig{})
}

func resultPaths(results []Result) map[string]bool {
	paths := make(map[string]bool, len(results))
	for _, r := range results {
		paths[r.Path] = true
	}
	return paths
}

// The vector leg ranks BM25's candidates only: the nearest vector in the
// corpus must not surface when its document shares no term with the query.
func TestHybridVectorLegExcludesDocumentsWithoutLexicalMatch(t *testing.T) {
	_, hybrid := seedCandidateCorpus(t)
	results, err := hybrid.Search(context.Background(), SearchOpts{Query: "language", TopK: 10, Explain: true})
	if err != nil {
		t.Fatal(err)
	}
	paths := resultPaths(results)
	if paths["rust.md"] {
		t.Fatalf("document without a lexical match surfaced: %+v", results)
	}
	if !paths["go.md"] || !paths["python.md"] {
		t.Fatalf("lexical candidates missing: %+v", results)
	}
	for _, r := range results {
		if r.Explain == nil || r.Explain.VectorRank == 0 {
			t.Fatalf("candidate %s was not ranked by the vector leg: %+v", r.Path, r.Explain)
		}
	}
}

// A strict conjunction that matches one document widens the vector leg's
// candidates to documents matching any query term, so a short lexical pool
// does not cap what the vector leg can contribute.
func TestHybridWidensShortLexicalPoolForVectorLeg(t *testing.T) {
	_, hybrid := seedCandidateCorpus(t)
	results, err := hybrid.Search(context.Background(), SearchOpts{Query: "compiled language", TopK: 10})
	if err != nil {
		t.Fatal(err)
	}
	paths := resultPaths(results)
	if !paths["go.md"] || !paths["python.md"] {
		t.Fatalf("want the strict match and the widened candidate, got %+v", results)
	}
	if paths["rust.md"] {
		t.Fatalf("widening admitted a document matching no query term: %+v", results)
	}
}

// With no lexical match anywhere, the vector leg still scans the corpus, so a
// purely semantic query is not answered with nothing.
func TestHybridFallsBackToFullScanWithoutLexicalMatches(t *testing.T) {
	_, hybrid := seedCandidateCorpus(t)
	results, err := hybrid.Search(context.Background(), SearchOpts{Query: "lifetimes", TopK: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 || results[0].Path != "rust.md" {
		t.Fatalf("want the nearest vector first from the full scan, got %+v", results)
	}
}

// The bounded scan keeps the scan's contracts: scope glob before the
// per-document collapse, and documents outside the candidate list excluded.
func TestVectorSearchDocsRespectsCandidatesAndScope(t *testing.T) {
	database, _ := seedCandidateCorpus(t)
	vs := NewVectorSearch(database, "fp")
	ctx := context.Background()

	results, err := vs.SearchDocs(ctx, []float32{1, 0}, 10, SearchOpts{}, []int64{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Path != "python.md" || results[1].Path != "go.md" {
		t.Fatalf("want python.md then go.md, got %+v", results)
	}

	results, err = vs.SearchDocs(ctx, []float32{1, 0}, 10, SearchOpts{Path: "go.md"}, []int64{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Path != "go.md" {
		t.Fatalf("scope glob not applied to the bounded scan: %+v", results)
	}

	results, err = vs.SearchDocs(ctx, []float32{1, 0}, 10, SearchOpts{}, nil)
	if err != nil || len(results) != 0 {
		t.Fatalf("no candidates must mean no results, got %+v err=%v", results, err)
	}
}
