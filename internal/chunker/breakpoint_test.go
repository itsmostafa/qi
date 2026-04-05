package chunker

import (
	"strings"
	"testing"

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
