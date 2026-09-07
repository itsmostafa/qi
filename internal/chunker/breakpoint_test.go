package chunker

import (
	"strings"
	"testing"
	"time"

	"github.com/itsmostafa/qi/internal/parser"
)

func TestBreakpointChunker_EmptyDoc(t *testing.T) {
	c := NewBreakpointChunker(256)
	doc := &parser.Document{Sections: nil}
	chunks := c.Chunk(doc)
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks for empty doc, got %d", len(chunks))
	}
}

func TestBreakpointChunker_SingleSmallSection(t *testing.T) {
	c := NewBreakpointChunker(512)
	doc := &parser.Document{
		Sections: []parser.Section{
			{Text: "Hello world", HeadingPath: "Intro"},
		},
	}
	chunks := c.Chunk(doc)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].HeadingPath != "Intro" {
		t.Errorf("expected heading 'Intro', got %q", chunks[0].HeadingPath)
	}
}

func TestBreakpointChunker_LargeTextSplits(t *testing.T) {
	c := NewBreakpointChunker(64)
	// Generate text much larger than target
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = strings.Repeat("word ", 10)
	}
	doc := &parser.Document{
		Sections: []parser.Section{
			{Text: strings.Join(lines, "\n")},
		},
	}
	chunks := c.Chunk(doc)
	if len(chunks) < 2 {
		t.Errorf("expected multiple chunks for large text, got %d", len(chunks))
	}
}

func TestBreakpointChunker_SequenceNumbers(t *testing.T) {
	c := NewBreakpointChunker(32)
	doc := &parser.Document{
		Sections: []parser.Section{
			{Text: strings.Repeat("abc\n", 40), HeadingPath: "A"},
			{Text: strings.Repeat("xyz\n", 40), HeadingPath: "B"},
		},
	}
	chunks := c.Chunk(doc)
	for i, ch := range chunks {
		if ch.Seq != i {
			t.Errorf("chunk[%d].Seq = %d, want %d", i, ch.Seq, i)
		}
	}
}

func TestBreakpointChunker_OversizedSingleLine(t *testing.T) {
	c := NewBreakpointChunker(64)
	// Single line 4x longer than target — simulates a minified file with no newlines.
	longLine := strings.Repeat("x", 256)
	doc := &parser.Document{
		Sections: []parser.Section{
			{Text: longLine, HeadingPath: "file.min.js"},
		},
	}
	chunks := c.Chunk(doc)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks for oversized single line, got %d", len(chunks))
	}
	for _, ch := range chunks {
		if len([]rune(ch.Text)) > 64 {
			t.Errorf("chunk exceeds target size: %d runes", len([]rune(ch.Text)))
		}
	}
	// Reassemble and verify no content is lost.
	var sb strings.Builder
	for _, ch := range chunks {
		sb.WriteString(ch.Text)
	}
	if sb.String() != longLine {
		t.Errorf("reassembled text does not match original")
	}
}

func TestBreakpointChunker_PreservesHeadingPath(t *testing.T) {
	c := NewBreakpointChunker(256)
	doc := &parser.Document{
		Sections: []parser.Section{
			{Text: "some text", HeadingPath: "Chapter > Section"},
		},
	}
	chunks := c.Chunk(doc)
	for _, ch := range chunks {
		if ch.HeadingPath != "Chapter > Section" {
			t.Errorf("expected heading path preserved, got %q", ch.HeadingPath)
		}
	}
}

// A non-positive target size used to spin forever in the oversized-line split
// loop (offset += TargetSize). The timeout is the assertion.
func TestBreakpointChunker_NonPositiveTargetSize(t *testing.T) {
	done := make(chan int, 1)
	go func() {
		doc := &parser.Document{Sections: []parser.Section{{Text: "hello world"}}}
		done <- len(NewBreakpointChunker(0).Chunk(doc))
	}()
	select {
	case n := <-done:
		if n == 0 {
			t.Error("expected at least one chunk")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Chunk hung with targetSize 0")
	}
}

func TestBreakpointChunkerCitesEachMappedChunk(t *testing.T) {
	src := "# Heading\n\nfirst café\n\nsecond passage\n\n## Later\n\nthird text\n"
	doc, err := parser.For(".md").Parse("note.md", []byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	chunks := NewBreakpointChunker(10).Chunk(doc)
	if len(chunks) < 2 {
		t.Fatalf("got %d chunks, want several", len(chunks))
	}
	for _, ch := range chunks {
		if ch.StartLine < 1 || ch.EndLine < ch.StartLine {
			t.Errorf("invalid citation range %+v", ch)
		}
		if ch.Ordinal < 0 || ch.Ordinal >= len(src) {
			t.Errorf("invalid raw ordinal %+v", ch)
		}
	}
	if chunks[0].Ordinal == chunks[len(chunks)-1].Ordinal {
		t.Errorf("all chunks point at one source location: %+v", chunks)
	}
}

func TestBreakpointChunkerNarrowsFlattenedMarkdownLines(t *testing.T) {
	src := "# Heading\n\nαααααααα\nββββββββ\n"
	doc, err := parser.For(".md").Parse("note.md", []byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	chunks := NewBreakpointChunker(8).Chunk(doc)
	if len(chunks) < 2 {
		t.Fatalf("got %d chunks, want flattened paragraph split", len(chunks))
	}
	if chunks[0].StartLine != 3 || chunks[len(chunks)-1].EndLine != 4 {
		t.Errorf("flattened paragraph citations = %+v, want lines 3 through 4", chunks)
	}
	if chunks[0].Ordinal >= chunks[len(chunks)-1].Ordinal {
		t.Errorf("flattened chunks did not narrow raw ordinal: %+v", chunks)
	}
	// A split after the synthetic soft-break space must not cite the next line.
	chunks = NewBreakpointChunker(9).Chunk(doc)
	if len(chunks) != 2 || chunks[0].EndLine != 3 || chunks[1].StartLine != 4 {
		t.Errorf("soft-break boundary widened source ranges: %+v", chunks)
	}
}

func TestBreakpointChunkerCitesLaterCodeChunksPrecisely(t *testing.T) {
	src := "# Code\n\n```\nline-one\nline-two\nline-333\n```\n"
	doc, err := parser.For(".md").Parse("note.md", []byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	chunks := NewBreakpointChunker(8).Chunk(doc)
	if len(chunks) != 3 {
		t.Fatalf("got %d code chunks, want 3: %+v", len(chunks), chunks)
	}
	for i, wantLine := range []int{4, 5, 6} {
		if chunks[i].StartLine != wantLine || chunks[i].EndLine != wantLine {
			t.Errorf("chunk %d range = %d-%d, want %d-%d", i, chunks[i].StartLine, chunks[i].EndLine, wantLine, wantLine)
		}
	}
	if chunks[1].Ordinal <= chunks[0].Ordinal || chunks[2].Ordinal <= chunks[1].Ordinal {
		t.Errorf("code chunk ordinals are not source ordered: %+v", chunks)
	}
}

func TestTrimmedCodeChunksKeepExactRawOrdinals(t *testing.T) {
	src := "# Code\n\n```\n  ééééé  \n```\n"
	doc, err := parser.For(".md").Parse("note.md", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	chunks := NewBreakpointChunker(2).Chunk(doc)
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3: %+v", len(chunks), chunks)
	}
	for i, ch := range chunks {
		want := strings.Index(src, "é") + i*4
		if ch.Ordinal != want || ch.StartLine != 4 || ch.EndLine != 4 {
			t.Errorf("chunk %d = %+v, want byte %d on line 4", i, ch, want)
		}
	}
}

func TestBreakpointChunkerUnicodeOrdinalUsesRawBytes(t *testing.T) {
	src := "ééééé\n"
	doc, err := parser.For(".txt").Parse("note.txt", []byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	chunks := NewBreakpointChunker(2).Chunk(doc)
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}
	for i, want := range []int{0, 4, 8} {
		if chunks[i].Ordinal != want || chunks[i].StartLine != 1 || chunks[i].EndLine != 1 {
			t.Errorf("chunk %d citation = %+v, want ordinal %d on line 1", i, chunks[i], want)
		}
	}
}
