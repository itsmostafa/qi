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
