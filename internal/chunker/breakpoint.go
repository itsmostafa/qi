package chunker

import (
	"strings"
	"unicode"

	"github.com/itsmostafa/qi/internal/parser"
)

// breakpointScores assigns a score to each position that could start a new chunk.
// Higher scores = stronger break points.
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

func NewBreakpointChunker(targetSize int) *BreakpointChunker {
	return &BreakpointChunker{
		TargetSize: targetSize,
		MinSize:    targetSize / 4,
	}
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

	type breakPoint struct {
		lineIdx int
		score   int
	}

	// Score each line boundary
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
		size += runeLen(line) + 1 // +1 for newline

		if size < c.MinSize {
			continue
		}

		// Apply distance decay: score decreases as we move away from target
		if size >= c.TargetSize || (scores[i] > 0 && size >= c.MinSize) {
			decay := distanceDecay(size, c.TargetSize)
			effectiveScore := float64(scores[i]) * decay

			// Emit chunk when we hit target size OR have a strong breakpoint
			if size >= c.TargetSize || effectiveScore >= float64(scoreBlankLine) {
				text := strings.Join(lines[start:i+1], "\n")
				text = strings.TrimRightFunc(text, unicode.IsSpace)
				if text != "" {
					chunks = append(chunks, Chunk{
						Seq:         seq,
						Text:        text,
						HeadingPath: section.HeadingPath,
						Ordinal:     section.Ordinal,
					})
					seq++
				}
				start = i + 1
				size = 0
			}
		}
	}

	// Remaining text
	if start < len(lines) {
		text := strings.Join(lines[start:], "\n")
		text = strings.TrimRightFunc(text, unicode.IsSpace)
		if text != "" {
			chunks = append(chunks, Chunk{
				Seq:         seq,
				Text:        text,
				HeadingPath: section.HeadingPath,
				Ordinal:     section.Ordinal,
			})
		}
	}

	return chunks
}

func runeLen(s string) int {
	return len([]rune(s))
}

// distanceDecay returns a multiplier [0, 1] based on how far size is from target.
// At target: 1.0. Decays linearly.
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
