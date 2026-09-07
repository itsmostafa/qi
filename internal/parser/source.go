package parser

import (
	"bytes"
	"sort"
)

// sourceMapper converts offsets in the original input to source spans. Keeping
// this map next to parsing is important: rendered Markdown is not reversible.
type sourceMapper struct {
	starts []int
}

func newSourceMapper(data []byte) sourceMapper {
	starts := []int{0}
	for i, b := range data {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	return sourceMapper{starts: starts}
}

func (m sourceMapper) lineAt(offset int) int {
	if offset < 0 {
		offset = 0
	}
	line := sort.Search(len(m.starts), func(i int) bool { return m.starts[i] > offset })
	if line == 0 {
		return 1
	}
	return line
}

func (m sourceMapper) span(start, end int) SourceSpan {
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	return SourceSpan{Start: start, End: end, StartLine: m.lineAt(start), EndLine: m.lineAt(max(start, end-1))}
}

// rawLineMap maps plaintext transformed bytes to source line intervals. It is
// linear in the input and bounded by line count, not rune count.
func rawLineMap(data []byte, m sourceMapper) []SourceInterval {
	var out []SourceInterval
	start := 0
	for start < len(data) {
		end := len(data)
		if i := bytes.IndexByte(data[start:], '\n'); i >= 0 {
			end = start + i + 1
		}
		out = append(out, SourceInterval{TextStart: start, TextEnd: end, Literal: true, SourceSpan: m.span(start, end)})
		start = end
	}
	return out
}

// Clip restricts a mapping to an overlapping transformed byte interval.
// Literal intervals stay on one source line, so clipping adjusts bytes only;
// transformed intervals retain their conservative source envelope.
func (s SourceInterval) Clip(start, end int) SourceInterval {
	from, to := max(start, s.TextStart), min(end, s.TextEnd)
	if s.Literal {
		s.Start += from - s.TextStart
		s.End -= s.TextEnd - to
	}
	s.TextStart, s.TextEnd = from, to
	return s
}

// SourceRange combines the source spans overlapping transformed bytes [start,end).
// Maps are ordered by transformed position, even when source fragments reorder.
func SourceRange(intervals []SourceInterval, start, end int) (SourceSpan, bool) {
	if end <= start {
		return SourceSpan{}, false
	}
	i := sort.Search(len(intervals), func(i int) bool { return intervals[i].TextEnd > start })
	var out SourceSpan
	found := false
	for ; i < len(intervals) && intervals[i].TextStart < end; i++ {
		span := intervals[i].Clip(start, end).SourceSpan
		if !found {
			out, found = span, true
			continue
		}
		out.Start, out.End = min(out.Start, span.Start), max(out.End, span.End)
		out.StartLine, out.EndLine = min(out.StartLine, span.StartLine), max(out.EndLine, span.EndLine)
	}
	return out, found
}
