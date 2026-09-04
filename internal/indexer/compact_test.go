package indexer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/itsmostafa/qi/internal/config"
)

// Reindexing churn used to grow the file without bound: FTS5 never merged its
// segments and freed pages were never returned. 188 MB held 200 KB of notes.
func TestIndexReclaimsSpaceAfterChurn(t *testing.T) {
	database := openTestDB(t)
	dir := t.TempDir()
	col := config.Collection{Name: "test", Path: dir, Extensions: []string{".md"}}
	idx := New(database, 512)
	ctx := context.Background()

	write := func(round int) {
		for i := 0; i < 40; i++ {
			body := strings.Repeat(fmt.Sprintf("round%d document%d searchable prose. ", round, i), 60)
			path := filepath.Join(dir, fmt.Sprintf("note%02d.md", i))
			if err := os.WriteFile(path, []byte("# Note\n\n"+body), 0o640); err != nil {
				t.Fatal(err)
			}
		}
	}

	for round := 0; round < 6; round++ {
		write(round)
		if _, err := idx.Index(ctx, col); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
	}

	var pages, freelist int64
	if err := database.QueryRowContext(ctx,
		`SELECT * FROM pragma_page_count(), pragma_freelist_count()`).Scan(&pages, &freelist); err != nil {
		t.Fatal(err)
	}
	if freelist > pages/4 && freelist >= 1000 {
		t.Errorf("dead space left unreclaimed: %d of %d pages free", freelist, pages)
	}

	// Every rewrite supersedes a body; none of them may survive unreferenced.
	var orphans int
	if err := database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM content
		WHERE hash NOT IN (SELECT DISTINCT content_hash FROM documents)`).Scan(&orphans); err != nil {
		t.Fatal(err)
	}
	if orphans != 0 {
		t.Errorf("%d superseded content bodies retained", orphans)
	}

	// Merged segments, not one per write.
	var segments int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM chunks_fts_data`).Scan(&segments); err != nil {
		t.Fatal(err)
	}
	if segments > 100 {
		t.Errorf("chunks_fts_data holds %d rows; segments were not merged", segments)
	}
}

func TestCompactKeepsSearchWorking(t *testing.T) {
	database := openTestDB(t)
	col := makeTestCollection(t, map[string]string{
		"a.md": "# Alpha\n\nfindmeplease unique text\n",
	})
	if _, err := New(database, 512).Index(context.Background(), col); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := database.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM chunks_fts WHERE chunks_fts MATCH 'findmeplease'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Error("optimize left the FTS index unsearchable")
	}
}

// Change detection is by content hash, so a parser fix would never reach files
// already indexed. --force is the only way to rebuild them.
func TestForceReindexesUnchangedFiles(t *testing.T) {
	database := openTestDB(t)
	col := makeTestCollection(t, map[string]string{"a.md": "# A\n\nbody\n"})
	ctx := context.Background()
	idx := New(database, 512)

	if _, err := idx.Index(ctx, col); err != nil {
		t.Fatal(err)
	}
	var before int64
	if err := database.QueryRowContext(ctx, `SELECT MAX(id) FROM chunks`).Scan(&before); err != nil {
		t.Fatal(err)
	}

	stats, err := idx.Index(ctx, col)
	if err != nil {
		t.Fatal(err)
	}
	if stats.FilesUpdated != 0 {
		t.Errorf("unchanged file was reindexed without --force: %+v", stats)
	}

	idx.Force = true
	stats, err = idx.Index(ctx, col)
	if err != nil {
		t.Fatal(err)
	}
	if stats.FilesUpdated != 1 {
		t.Errorf("--force did not reindex: %+v", stats)
	}
	var after int64
	if err := database.QueryRowContext(ctx, `SELECT MAX(id) FROM chunks`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after <= before {
		t.Error("--force did not rebuild chunks")
	}
}
