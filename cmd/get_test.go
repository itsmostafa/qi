package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/itsmostafa/qi/internal/db"
)

// makeGetTestConfig indexes two documents whose hashes share a 6-char prefix,
// plus one unique document with a NULL title.
func makeGetTestConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "qi.db")
	database, err := db.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO content(hash, body) VALUES
			('abc123aaaa', 'one'),
			('abc123bbbb', 'two'),
			('def456cccc', 'l1
l2
l3
l4
');
		INSERT INTO documents(collection, path, title, content_hash) VALUES
			('test', 'a.md', 'Doc A', 'abc123aaaa'),
			('test', 'b.md', 'Doc B', 'abc123bbbb'),
			('test', 'c.md', NULL, 'def456cccc');
	`); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	return writeIndexTestConfig(t, fmt.Sprintf("database_path: %s\n", dbPath))
}

func withGetFlags(t *testing.T, lines string, maxBytes int, f string) {
	t.Helper()
	oldLines, oldMax, oldFormat := getLines, getMaxBytes, format
	getLines, getMaxBytes, format = lines, maxBytes, f
	t.Cleanup(func() { getLines, getMaxBytes, format = oldLines, oldMax, oldFormat })
}

// An ambiguous prefix used to print up to five whole documents.
func TestGetAmbiguousPrefixErrors(t *testing.T) {
	withIndexTestConfig(t, makeGetTestConfig(t))
	withGetFlags(t, "", 0, "text")

	var err error
	out := captureIndexTestOutput(t, func() { err = getCmd.RunE(getCmd, []string{"abc123"}) })
	if err == nil {
		t.Fatalf("ambiguous prefix should error, got output:\n%s", out)
	}
	if strings.Contains(out, "one") || strings.Contains(out, "two") {
		t.Errorf("ambiguous prefix printed document bodies:\n%s", out)
	}
	for _, want := range []string{"ambiguous", "abc123aaaa", "abc123bbbb", "qi://test/a.md"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q: %v", want, err)
		}
	}
}

func TestGetLineRangeAndMaxBytes(t *testing.T) {
	withIndexTestConfig(t, makeGetTestConfig(t))

	withGetFlags(t, "2:3", 0, "text")
	var err error
	out := captureIndexTestOutput(t, func() { err = getCmd.RunE(getCmd, []string{"def456"}) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "l2\nl3\n") || strings.Contains(out, "l1") || strings.Contains(out, "l4") {
		t.Errorf("--lines 2:3 did not bound the body:\n%s", out)
	}

	withGetFlags(t, "", 2, "text")
	out = captureIndexTestOutput(t, func() { err = getCmd.RunE(getCmd, []string{"def456"}) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[truncated: 2 of 12 bytes]") {
		t.Errorf("--max-bytes did not report truncation:\n%s", out)
	}

	withGetFlags(t, "", 2, "json")
	out = captureIndexTestOutput(t, func() { err = getCmd.RunE(getCmd, []string{"def456"}) })
	if err != nil {
		t.Fatal(err)
	}
	var doc candidate
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("--format json is not valid JSON: %v\n%s", err, out)
	}
	if doc.Body != "l1" || !doc.Truncated || doc.Hash != "def456cccc" {
		t.Errorf("unexpected JSON document: %+v", doc)
	}
}

func TestGetLineRangeRejectsBadSpec(t *testing.T) {
	for _, spec := range []string{"3", "0:2", "5:2", "99:100"} {
		if _, err := sliceLines("a\nb\nc\n", spec); err == nil {
			t.Errorf("--lines %q should be rejected", spec)
		}
	}
}

// Two files with identical bytes dedupe to one content row, so both documents
// carry the same hash. No prefix can tell them apart — not even all 64
// characters — so `qi get` must return the shared content instead of demanding
// a longer prefix that cannot exist.
func TestGetReturnsSharedContentForDuplicateDocuments(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "qi.db")
	database, err := db.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	const hash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if _, err := database.Exec(
		`INSERT INTO content(hash, body) VALUES (?, 'shared body')`, hash); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO documents(collection, path, title, content_hash)
		VALUES ('test', 'a.md', 'Doc A', ?), ('test', 'b.md', 'Doc B', ?)`, hash, hash); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	withIndexTestConfig(t, writeIndexTestConfig(t, fmt.Sprintf("database_path: %s\n", dbPath)))
	withGetFlags(t, "", 0, "text")

	out := captureIndexTestOutput(t, func() { err = getCmd.RunE(getCmd, []string{hash}) })
	if err != nil {
		t.Fatalf("a fully-qualified hash must resolve, got: %v", err)
	}
	for _, want := range []string{"shared body", "qi://test/a.md", "Also at:    qi://test/b.md"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	// A prefix spanning genuinely different hashes is still ambiguous.
	withIndexTestConfig(t, makeGetTestConfig(t))
	if err := getCmd.RunE(getCmd, []string{"abc123"}); err == nil {
		t.Error("a prefix matching two distinct hashes must still error")
	}
}
