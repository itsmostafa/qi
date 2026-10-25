package indexer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/itsmostafa/qi/internal/config"
	"github.com/itsmostafa/qi/internal/db"
	"github.com/ncruces/go-sqlite3/driver"
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

// A post-merge hook runs qi index after every pull, each touching a few files.
// compact must not pay for a full optimize on every such run, yet the replaced
// postings those runs leave behind must not pile up without bound either:
// automerge alone never reaches the big bottom segment that holds them.
func TestSmallReindexRunsKeepFTSIndexBounded(t *testing.T) {
	database := openTestDB(t)
	dir := t.TempDir()
	col := config.Collection{Name: "test", Path: dir, Extensions: []string{".md"}}
	idx := New(database, 512)
	ctx := context.Background()

	write := func(i, rev int) {
		writeNote(t, dir, fmt.Sprintf("note%02d.md", i), fmt.Sprintf("doc%d rev%d", i, rev), 200, "")
	}
	ftsBytes := func() int64 {
		var n int64
		if err := database.QueryRowContext(ctx,
			`SELECT SUM(length(block)) FROM chunks_fts_data`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	const docs = 40
	for i := 0; i < docs; i++ {
		write(i, 0)
	}
	if _, err := idx.Index(ctx, col); err != nil {
		t.Fatal(err)
	}
	// Start from one bottom segment, as a long-lived index has, so automerge
	// cannot reclaim the churn below on its own.
	optimizeFTS(t, database)

	// 60 runs of 4 files each rewrite the corpus six times over.
	for run := 1; run <= 60; run++ {
		for k := 0; k < 4; k++ {
			write((run*4+k)%docs, run)
		}
		if _, err := idx.Index(ctx, col); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
	}

	churned := ftsBytes()
	optimizeFTS(t, database)
	if optimal := ftsBytes(); churned > optimal*3/2 {
		t.Errorf("fts index is %d bytes after small reindex runs, %d once optimized; replaced postings were not reclaimed",
			churned, optimal)
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

// ftsBlocksContaining counts chunks_fts_data blocks holding token's bytes —
// what anyone reading the file would find, whether or not MATCH still does.
func ftsBlocksContaining(t *testing.T, database *db.DB, token string) int {
	t.Helper()
	var n int
	if err := database.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM chunks_fts_data WHERE instr(block, CAST(? AS BLOB)) > 0`, token).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// compact hard-deletes documents and prunes content so removed text leaves the
// database. A plain FTS5 delete writes a marker and leaves the text in the
// index until an optimize rewrites its segment, which a small run never
// triggers; secure-delete removes the entries in place.
func TestRemovedTextLeavesFTSIndex(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "qi.db")
	open := func() *db.DB {
		database, err := db.Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = database.Close() })
		return database
	}
	database := open()
	dir := t.TempDir()
	col := config.Collection{Name: "test", Path: dir, Extensions: []string{".md"}}

	// Leaf pages prefix-compress each term against the one before it, so look
	// for the distinctive tail of a token rather than the whole of it.
	const deleted, redacted = "xqdeletedtoken987", "zqredactedtoken654"
	tail := func(tok string) string { return tok[2:] }
	name := func(i int) string { return fmt.Sprintf("n%03d.md", i) }
	for i := 0; i < 200; i++ {
		extra := ""
		switch i {
		case 7:
			extra = deleted
		case 80:
			extra = redacted
		}
		writeNote(t, dir, name(i), fmt.Sprintf("doc%d common text", i), 300, extra)
	}
	if _, err := New(database, 512).Index(ctx, col); err != nil {
		t.Fatal(err)
	}
	for _, tok := range []string{deleted, redacted} {
		if ftsBlocksContaining(t, database, tail(tok)) == 0 {
			t.Fatalf("%s not found in the fts index before removal; the search cannot detect a leak", tok)
		}
	}

	// secure-delete is a persistent table option, not a connection setting.
	_ = database.Close()
	database = open()

	// A small run — too small to reach the optimize threshold — deletes the
	// file holding one token and edits the other token out of its file.
	if err := os.Remove(filepath.Join(dir, name(7))); err != nil {
		t.Fatal(err)
	}
	writeNote(t, dir, name(80), "doc80 common text", 300, "")
	if _, err := New(database, 512).Index(ctx, col); err != nil {
		t.Fatal(err)
	}

	for _, tok := range []string{deleted, redacted} {
		if n := ftsBlocksContaining(t, database, tail(tok)); n != 0 {
			t.Errorf("%s still present in %d chunks_fts_data blocks after its text was removed", tok, n)
		}
	}
	if _, err := database.ExecContext(ctx,
		`INSERT INTO chunks_fts(chunks_fts) VALUES('integrity-check')`); err != nil {
		t.Errorf("fts integrity-check: %v", err)
	}
}

// A run that changes a few files on a large index must not rewrite the whole
// index: the new segment stays below the optimize threshold.
func TestSmallReindexRunSkipsOptimize(t *testing.T) {
	database := openTestDB(t)
	dir := t.TempDir()
	col := config.Collection{Name: "test", Path: dir, Extensions: []string{".md"}}
	idx := New(database, 512)
	ctx := context.Background()

	write := func(i, rev int) {
		writeNote(t, dir, fmt.Sprintf("n%03d.md", i), fmt.Sprintf("doc%d rev%d", i, rev), 300, "")
	}
	for i := 0; i < 200; i++ {
		write(i, 0)
	}
	if _, err := idx.Index(ctx, col); err != nil {
		t.Fatal(err)
	}
	optimizeFTS(t, database)
	largest, rest, err := ftsSegmentPages(ctx, database.DB)
	if err != nil {
		t.Fatal(err)
	}
	if rest != 0 || largest == 0 {
		t.Fatalf("optimized index: largest=%d rest=%d, want one segment", largest, rest)
	}

	write(3, 1)
	if _, err := idx.Index(ctx, col); err != nil {
		t.Fatal(err)
	}
	largest, rest, err = ftsSegmentPages(ctx, database.DB)
	if err != nil {
		t.Fatal(err)
	}
	if rest == 0 || largest <= rest*4 {
		t.Errorf("after a 1-file run: largest=%d rest=%d; want the new segment left unmerged, below the threshold", largest, rest)
	}
}

func TestFTSSegmentPagesEmptyIndex(t *testing.T) {
	ctx := context.Background()
	largest, rest, err := ftsSegmentPages(ctx, openTestDB(t).DB)
	if largest != 0 || rest != 0 || err != nil {
		t.Errorf("migrated empty index: got (%d, %d, %v), want (0, 0, nil)", largest, rest, err)
	}

	raw, err := driver.Open(filepath.Join(t.TempDir(), "raw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.Exec(`CREATE VIRTUAL TABLE chunks_fts USING fts5(text)`); err != nil {
		t.Fatal(err)
	}
	largest, rest, err = ftsSegmentPages(ctx, raw)
	if largest != 0 || rest != 0 || err != nil {
		t.Errorf("fresh fts5 table: got (%d, %d, %v), want (0, 0, nil)", largest, rest, err)
	}
}

func TestSqliteVarint(t *testing.T) {
	tests := []struct {
		in   []byte
		want uint64
		n    int
	}{
		{[]byte{0x05}, 5, 1},
		{[]byte{0x05, 0xff}, 5, 1},
		{[]byte{0x81, 0x00}, 128, 2},
		{[]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xab}, 0xFFFFFFFFFFFFFFAB, 9},
		{[]byte{0x81}, 0, 0},
		{[]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, 0, 0},
		{nil, 0, 0},
	}
	for _, tt := range tests {
		got, n := sqliteVarint(tt.in)
		if got != tt.want || n != tt.n {
			t.Errorf("sqliteVarint(% x) = (%#x, %d), want (%#x, %d)", tt.in, got, n, tt.want, tt.n)
		}
	}
}

// writeNote writes dir/name as a markdown note of words distinct words, each
// prefixed by tag, followed by extra.
func writeNote(t *testing.T, dir, name, tag string, words int, extra string) {
	t.Helper()
	var b strings.Builder
	for w := 0; w < words; w++ {
		fmt.Fprintf(&b, "%s word%d ", tag, w)
	}
	b.WriteString(extra)
	if err := os.WriteFile(filepath.Join(dir, name), []byte("# Note\n\n"+b.String()), 0o640); err != nil {
		t.Fatal(err)
	}
}

// optimizeFTS merges the whole FTS index into one segment.
func optimizeFTS(t *testing.T, database *db.DB) {
	t.Helper()
	if _, err := database.ExecContext(context.Background(),
		`INSERT INTO chunks_fts(chunks_fts) VALUES('optimize')`); err != nil {
		t.Fatal(err)
	}
}
