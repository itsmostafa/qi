package search

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/itsmostafa/qi/internal/config"
	"github.com/itsmostafa/qi/internal/providers"
)

func TestCitationSearchSkipsInvalidRanges(t *testing.T) {
	for _, route := range []string{"bm25", "vector"} {
		t.Run(route, func(t *testing.T) {
			ctx := context.Background()
			database := openTestDB(t)
			seedTestData(t, database)
			if _, err := database.ExecContext(ctx, `UPDATE chunks SET text=?, start_line=NULL, end_line=NULL WHERE id=2`, "programming "+strings.Repeat("filler ", 100)); err != nil {
				t.Fatal(err)
			}
			for id, vector := range map[int64][]float32{1: {1, 0}, 2: {0, 1}} {
				if err := database.UpsertEmbedding(ctx, id, vector, "test", "model", 2, "fp"); err != nil {
					t.Fatal(err)
				}
			}
			for _, tc := range []struct {
				name     string
				sql      string
				limit    int
				passages int
				wantNone bool
			}{
				{name: "unknown outside pool", limit: 1},
				{name: "unknown selected primary", limit: 2},
				{name: "unknown unrequested sibling", sql: `UPDATE chunks SET doc_id=1, content_hash='hash1', seq=1 WHERE id=2`, limit: 1},
				{name: "unknown requested sibling", limit: 1, passages: 1},
				{name: "inverted requested sibling", sql: `UPDATE chunks SET start_line=5, end_line=2 WHERE id=2`, limit: 1, passages: 1},
				{name: "unknown best primary", sql: `UPDATE chunks SET start_line=NULL, end_line=NULL WHERE id=1`, limit: 1, wantNone: true},
				{name: "valid sibling replaces primary", sql: `UPDATE chunks SET start_line=2, end_line=3 WHERE id=2`, limit: 1},
			} {
				t.Run(tc.name, func(t *testing.T) {
					if tc.sql != "" {
						if _, err := database.ExecContext(ctx, tc.sql); err != nil {
							t.Fatal(err)
						}
					}
					opts := SearchOpts{Query: "programming", TopK: tc.limit, Pool: tc.limit, Passages: tc.passages}
					var results []Result
					var err error
					if route == "bm25" {
						results, err = NewBM25(database).Search(ctx, opts)
					} else {
						results, err = NewVectorSearch(database, "fp").Search(ctx, []float32{1, 0}, tc.limit, opts)
					}
					if tc.wantNone {
						if err != nil || len(results) != 0 {
							t.Fatalf("expected no citable results, got results=%+v err=%v", results, err)
						}
					} else if err != nil || len(results) != 1 || results[0].DocID != 1 || len(results[0].Passages) != 0 {
						t.Fatalf("unexpected results=%+v err=%v", results, err)
					}
				})
			}
		})
	}
}

func TestHybridKeepsValidVectorHitsWithUnknownRanges(t *testing.T) {
	ctx := context.Background()
	database := openTestDB(t)
	seedTestData(t, database)
	if _, err := database.ExecContext(ctx, `
		UPDATE chunks SET text='semantic evidence', start_line=NULL, end_line=NULL WHERE id=2;
		INSERT INTO chunks(content_hash, doc_id, seq, text, content_length, start_line, end_line)
		VALUES ('hash2', 2, 1, 'valid semantic evidence', 23, 2, 3);
	`); err != nil {
		t.Fatal(err)
	}
	for id, vector := range map[int64][]float32{2: {1, 0}, 3: {1, 0.1}} {
		if err := database.UpsertEmbedding(ctx, id, vector, "test", "model", 2, "fp"); err != nil {
			t.Fatal(err)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[1,0]}]}`))
	}))
	defer srv.Close()
	embedder := providers.NewEmbedding(&config.EmbeddingProviderConfig{BaseURL: srv.URL, Model: "model", Dimension: 2})
	hybrid := NewHybrid(NewBM25(database), NewVectorSearch(database, "fp"), embedder, config.SearchConfig{VectorTopK: 1})
	results, err := hybrid.Search(ctx, SearchOpts{Query: "programming", TopK: 1, Passages: 1})
	if err != nil || len(results) != 2 {
		t.Fatalf("valid vector document lost: results=%+v err=%v", results, err)
	}
	for _, r := range results {
		if r.DocID == 2 && (r.ChunkID != 3 || len(r.Passages) != 0) {
			t.Fatalf("invalid vector evidence returned: %+v", r)
		}
	}
}

func TestFusionEvidenceIsOptInAndKeepsOtherPrimary(t *testing.T) {
	bm25 := []Result{{DocID: 1, ChunkID: 10, StartLine: 2, EndLine: 3}}
	vec := []Result{{DocID: 1, ChunkID: 20, StartLine: 8, EndLine: 9}}
	without := ReciprocalRankFusion(bm25, vec, 60, 0)
	with := ReciprocalRankFusion(bm25, vec, 60, 1)
	if len(without) != 1 || len(without[0].Passages) != 0 {
		t.Fatalf("unrequested evidence: %+v", without)
	}
	if len(with) != 1 || with[0].ChunkID != 10 || len(with[0].Passages) != 1 || with[0].Passages[0].ChunkID != 20 || with[0].Passages[0].StartLine != 8 || with[0].Passages[0].EndLine != 9 {
		t.Fatalf("other route's primary lost: %+v", with)
	}
	if with[0].Score != without[0].Score {
		t.Fatal("supporting evidence changed document rank")
	}
}
