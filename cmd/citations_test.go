package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type citationJSONResult struct {
	Hash      string            `json:"hash"`
	SourceURI string            `json:"source_uri"`
	StartLine int               `json:"start_line"`
	EndLine   int               `json:"end_line"`
	Passages  []json.RawMessage `json:"passages"`
}

func TestSearchJSONCitationRoundTripThroughGet(t *testing.T) {
	root := t.TempDir()
	collectionPath := filepath.Join(root, "docs")
	if err := os.Mkdir(collectionPath, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := "---\r\ntitle: Citation notes\r\ntags: [unicode]\r\n---\r\n# Intro\r\nIgnored context.\r\n\r\n## Evidence 🧭\r\nThe citation-anchor appears inside a Unicode section.\r\n\r\n## More evidence\r\nA second citation-anchor supports the same document.\r\n\r\n```go\r\nfmt.Println(\"fence\")\r\n```\r\n"
	nested := filepath.Join(collectionPath, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(nested, "space ü.md")
	if err := os.WriteFile(path, []byte(raw), 0o640); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeIndexTestConfig(t, fmt.Sprintf(`database_path: %s
search:
  default_mode: lexical
  chunk_size: 32
collections:
  - name: docs
    path: %s
    extensions: [.md]
`, filepath.Join(root, "qi.db"), collectionPath))
	withIndexTestConfig(t, cfgPath)

	oldForce := indexForce
	indexForce = false
	t.Cleanup(func() { indexForce = oldForce })
	if out := captureIndexTestOutput(t, func() {
		if err := indexCmd.RunE(indexCmd, []string{"docs"}); err != nil {
			t.Fatalf("index failed: %v", err)
		}
	}); out == "" {
		t.Fatal("index produced no status output")
	}

	oldFormat, oldCollection, oldTopK, oldPassages := format, searchCollection, searchTopK, searchPassages
	oldSince, oldUntil, oldSort := searchSince, searchUntil, searchSort
	format, searchCollection, searchTopK, searchPassages = "json", "", 10, 5
	searchSince, searchUntil, searchSort = "", "", ""
	t.Cleanup(func() {
		format, searchCollection, searchTopK, searchPassages = oldFormat, oldCollection, oldTopK, oldPassages
		searchSince, searchUntil, searchSort = oldSince, oldUntil, oldSort
	})
	var results []citationJSONResult
	searchOutput := captureIndexTestOutput(t, func() {
		if err := searchCmd.RunE(searchCmd, []string{"citation", "anchor"}); err != nil {
			t.Fatalf("search failed: %v", err)
		}
	})
	if err := json.Unmarshal([]byte(searchOutput), &results); err != nil {
		t.Fatalf("search JSON is invalid: %v\n%s", err, searchOutput)
	}
	if len(results) != 1 {
		t.Fatalf("search returned %d results, want one\n%s", len(results), searchOutput)
	}
	result := results[0]
	if len(result.Hash) != sha256.Size*2 {
		t.Fatalf("search hash = %q, want full SHA-256", result.Hash)
	}
	if result.SourceURI != "qi://docs/nested/space%20%C3%BC.md" {
		t.Fatalf("source_uri = %q, want escaped path identity", result.SourceURI)
	}
	if result.StartLine < 1 || result.EndLine < result.StartLine {
		t.Fatalf("invalid source range %d:%d", result.StartLine, result.EndLine)
	}
	if len(result.Passages) == 0 {
		t.Fatal("search result has no supporting passages")
	}

	// Get must serve the indexed content-addressed snapshot, not bytes changed
	// after indexing.
	changed := strings.Replace(raw, "citation-anchor", "changed-live", 1)
	if err := os.WriteFile(path, []byte(changed), 0o640); err != nil {
		t.Fatal(err)
	}
	oldGetLines, oldMaxBytes := getLines, getMaxBytes
	getLines, getMaxBytes, format = fmt.Sprintf("%d:%d", result.StartLine, result.EndLine), 0, "json"
	t.Cleanup(func() { getLines, getMaxBytes = oldGetLines, oldMaxBytes })
	var got candidate
	getOutput := captureIndexTestOutput(t, func() {
		if err := getCmd.RunE(getCmd, []string{result.Hash}); err != nil {
			t.Fatalf("get failed: %v", err)
		}
	})
	if err := json.Unmarshal([]byte(getOutput), &got); err != nil {
		t.Fatalf("get JSON is invalid: %v\n%s", err, getOutput)
	}
	rawLines := strings.Split(strings.TrimSuffix(raw, "\n"), "\n")
	if result.EndLine > len(rawLines) {
		t.Fatalf("source range ends past source: %d > %d", result.EndLine, len(rawLines))
	}
	expectedBody := strings.Join(rawLines[result.StartLine-1:result.EndLine], "\n")
	if got.Hash != result.Hash || got.Path != "nested/space ü.md" || got.SourceURI != result.SourceURI || got.Body != expectedBody || !strings.Contains(got.Body, "citation-anchor") || strings.Contains(got.Body, "changed-live") {
		t.Fatalf("get did not return indexed snapshot: hash=%q path=%q body=%q want=%q", got.Hash, got.Path, got.Body, expectedBody)
	}
}

func TestCitationRangesNarrowLateCRLFParagraphAndFenceChunks(t *testing.T) {
	root := t.TempDir()
	collectionPath := filepath.Join(root, "docs")
	if err := os.Mkdir(collectionPath, 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		"---", "title: Range notes", "---", "# Range heading",
		"paragraph first source line", "paragraph second source line", "paragraph third source line",
		"paragraph fourth source line", "paragraphmarkerNE near the end", "",
		"```text", "fence first source line", "fence second source line", "fence third source line",
		"fence fourth source line", "fencemarkerNE near the end", "```", "",
	}
	raw := strings.Join(lines, "\r\n")
	path := filepath.Join(collectionPath, "ranges.md")
	if err := os.WriteFile(path, []byte(raw), 0o640); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeIndexTestConfig(t, fmt.Sprintf(`database_path: %s
search:
  default_mode: lexical
  chunk_size: 36
collections:
  - name: docs
    path: %s
    extensions: [.md]
`, filepath.Join(root, "qi.db"), collectionPath))
	withIndexTestConfig(t, cfgPath)
	oldForce := indexForce
	indexForce = false
	t.Cleanup(func() { indexForce = oldForce })
	captureIndexTestOutput(t, func() {
		if err := indexCmd.RunE(indexCmd, []string{"docs"}); err != nil {
			t.Fatalf("index failed: %v", err)
		}
	})

	oldFormat, oldCollection, oldTopK, oldPassages := format, searchCollection, searchTopK, searchPassages
	oldSince, oldUntil, oldSort := searchSince, searchUntil, searchSort
	format, searchCollection, searchTopK, searchPassages = "json", "", 10, 0
	searchSince, searchUntil, searchSort = "", "", ""
	t.Cleanup(func() {
		format, searchCollection, searchTopK, searchPassages = oldFormat, oldCollection, oldTopK, oldPassages
		searchSince, searchUntil, searchSort = oldSince, oldUntil, oldSort
	})
	search := func(marker string) citationJSONResult {
		var results []citationJSONResult
		out := captureIndexTestOutput(t, func() {
			if err := searchCmd.RunE(searchCmd, []string{marker}); err != nil {
				t.Fatalf("search %q failed: %v", marker, err)
			}
		})
		if err := json.Unmarshal([]byte(out), &results); err != nil {
			t.Fatalf("search %q JSON is invalid: %v\n%s", marker, err, out)
		}
		if len(results) != 1 {
			t.Fatalf("search %q returned %d results, want one\n%s", marker, len(results), out)
		}
		return results[0]
	}
	paragraph := search("paragraphmarkerNE")
	if paragraph.StartLine <= 5 || paragraph.EndLine < paragraph.StartLine {
		t.Fatalf("late paragraph marker cited its first lines: %+v", paragraph)
	}
	fence := search("fencemarkerNE")
	if fence.StartLine <= 12 || fence.EndLine < fence.StartLine {
		t.Fatalf("late fence marker cited its first lines: %+v", fence)
	}
}

func TestIndexedDuplicateContentIsRetrievableAtAllPaths(t *testing.T) {
	root := t.TempDir()
	collectionPath := filepath.Join(root, "docs")
	if err := os.Mkdir(collectionPath, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("same content\n")
	for _, name := range []string{"one.md", "two.md"} {
		if err := os.WriteFile(filepath.Join(collectionPath, name), body, 0o640); err != nil {
			t.Fatal(err)
		}
	}
	cfgPath := writeIndexTestConfig(t, fmt.Sprintf(`database_path: %s
collections:
  - name: docs
    path: %s
    extensions: [.md]
`, filepath.Join(root, "qi.db"), collectionPath))
	withIndexTestConfig(t, cfgPath)
	oldForce := indexForce
	indexForce = false
	t.Cleanup(func() { indexForce = oldForce })
	captureIndexTestOutput(t, func() {
		if err := indexCmd.RunE(indexCmd, []string{"docs"}); err != nil {
			t.Fatalf("index failed: %v", err)
		}
	})

	hashBytes := sha256.Sum256(body)
	hash := hex.EncodeToString(hashBytes[:])
	oldFormat, oldLines, oldMaxBytes := format, getLines, getMaxBytes
	format, getLines, getMaxBytes = "json", "", 0
	t.Cleanup(func() { format, getLines, getMaxBytes = oldFormat, oldLines, oldMaxBytes })
	var got candidate
	out := captureIndexTestOutput(t, func() {
		if err := getCmd.RunE(getCmd, []string{hash}); err != nil {
			t.Fatalf("get failed: %v", err)
		}
	})
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("get JSON is invalid: %v\n%s", err, out)
	}
	if got.Hash != hash || got.Body != string(body) || len(got.AlsoAt) != 1 {
		t.Fatalf("unexpected duplicate retrieval: %+v", got)
	}
	if got.Path != "one.md" || got.AlsoAt[0] != "qi://docs/two.md" {
		t.Fatalf("duplicate paths lost identity: path=%q also_at=%q", got.Path, got.AlsoAt)
	}
}
