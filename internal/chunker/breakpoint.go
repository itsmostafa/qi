package chunker

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/itsmostafa/qi/internal/parser"
)

// breakpointScores assigns a score to each position that could start a new chunk.
const (
	scoreHeading   = 100
	scoreCodeFence = 80
	scoreBlankLine = 20
)

// BreakpointChunker splits sections using break-point scoring with distance decay.
type BreakpointChunker struct {
	TargetSize int // target chunk size in runes
	MinSize    int // minimum chunk size before emitting
}

const defaultTargetSize = 512

func NewBreakpointChunker(targetSize int) *BreakpointChunker {
	if targetSize <= 0 {
		targetSize = defaultTargetSize
	}
	return &BreakpointChunker{TargetSize: targetSize, MinSize: targetSize / 4}
}

func (c *BreakpointChunker) Chunk(doc *parser.Document) []Chunk {
	var chunks []Chunk
	seq := 0
	for _, section := range doc.Sections {
		sectionChunks := c.chunkSection(section, seq)
		chunks = append(chunks, sectionChunks...)
		seq += len(sectionChunks)
	}
	return chunks
}

func (c *BreakpointChunker) chunkSection(section parser.Section, startSeq int) []Chunk {
	lines := strings.Split(section.Text, "\n")
	if len(lines) == 0 {
		return nil
	}
	lineStarts := make([]int, len(lines))
	for i := 1; i < len(lines); i++ {
		lineStarts[i] = lineStarts[i-1] + len(lines[i-1]) + 1
	}
	makeChunk := func(text string, seq, byteStart int) Chunk {
		ch := Chunk{Seq: seq, Text: text, HeadingPath: section.HeadingPath, Ordinal: section.Ordinal}
		if span, ok := parser.SourceRange(section.SourceMap, byteStart, byteStart+len(text)); ok {
			ch.Ordinal, ch.StartLine, ch.EndLine = span.Start, span.StartLine, span.EndLine
		}
		return ch
	}

	scores := make([]int, len(lines))
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "#"):
			scores[i] = scoreHeading
		case strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~"):
			scores[i] = scoreCodeFence
		case trimmed == "":
			scores[i] = scoreBlankLine
		}
	}

	var chunks []Chunk
	seq := startSeq
	start := 0
	size := 0
	for i, line := range lines {
		lineLen := runeLen(line)
		if lineLen > c.TargetSize {
			if i > start {
				text := strings.TrimRightFunc(strings.Join(lines[start:i], "\n"), unicode.IsSpace)
				if text != "" {
					chunks = append(chunks, makeChunk(text, seq, lineStarts[start]))
					seq++
				}
			}
			byteStart, count := 0, 0
			for byteEnd := range line {
				if count == c.TargetSize {
					chunks = append(chunks, makeChunk(line[byteStart:byteEnd], seq, lineStarts[i]+byteStart))
					seq++
					byteStart, count = byteEnd, 0
				}
				count++
			}
			chunks = append(chunks, makeChunk(line[byteStart:], seq, lineStarts[i]+byteStart))
			seq++
			start = i + 1
			size = 0
			continue
		}

		size += lineLen + 1
		if size < c.MinSize {
			continue
		}
		if size >= c.TargetSize || (scores[i] > 0 && size >= c.MinSize) {
			decay := distanceDecay(size, c.TargetSize)
			effectiveScore := float64(scores[i]) * decay
			if size >= c.TargetSize || effectiveScore >= float64(scoreBlankLine) {
				text := strings.TrimRightFunc(strings.Join(lines[start:i+1], "\n"), unicode.IsSpace)
				if text != "" {
					chunks = append(chunks, makeChunk(text, seq, lineStarts[start]))
					seq++
				}
				start = i + 1
				size = 0
			}
		}
	}

	if start < len(lines) {
		text := strings.TrimRightFunc(strings.Join(lines[start:], "\n"), unicode.IsSpace)
		if text != "" {
			chunks = append(chunks, makeChunk(text, seq, lineStarts[start]))
		}
	}
	return chunks
}

func runeLen(s string) int { return utf8.RuneCountInString(s) }

func distanceDecay(size, target int) float64 {
	if target <= 0 {
		return 1.0
	}
	dist := size - target
	if dist < 0 {
		dist = -dist
	}
	decay := 1.0 - float64(dist)/float64(target)
	if decay < 0 {
		return 0
	}
	return decay
}
