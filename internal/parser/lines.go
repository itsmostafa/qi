package parser

import (
	"bytes"
	"strings"
)

// rawLine locates one source line. end excludes the line terminator (and a
// CR before it); next is where the following line starts.
type rawLine struct {
	start, end, next int
}

func splitRawLines(data []byte) []rawLine {
	var out []rawLine
	for start := 0; start < len(data); {
		next := len(data)
		end := len(data)
		if i := bytes.IndexByte(data[start:], '\n'); i >= 0 {
			end, next = start+i, start+i+1
		}
		if end > start && data[end-1] == '\r' {
			end--
		}
		out = append(out, rawLine{start: start, end: end, next: next})
		start = next
	}
	return out
}

// text is the line's content. A UTF-8 byte order mark before the first line
// is dropped, or it would hide a title or header on that line.
func (l rawLine) text(data []byte) string {
	t := string(data[l.start:l.end])
	if l.start == 0 {
		t = strings.TrimPrefix(t, "\ufeff")
	}
	return t
}

// lineSections builds sections for line-oriented markup (reStructuredText,
// AsciiDoc) whose body text is kept as source. Body lines are copied verbatim
// with literal per-line source intervals, so every chunk cites the raw lines it
// came from; heading lines are consumed into heading paths instead.
type lineSections struct {
	data         []byte
	mapper       sourceMapper
	sections     []Section
	path         []string
	levels       []int // levels[i] is the heading level of path[i]
	buf          mappedBuilder
	headings     []string
	headingSpans []SourceSpan
	// summaries counts leading metadata sections, which do not count as body
	// for the heading-only fallback.
	summaries int
}

func newLineSections(data []byte) *lineSections {
	return &lineSections{data: data, mapper: newSourceMapper(data)}
}

// line appends one raw source line, terminator included, to the current section.
func (s *lineSections) line(l rawLine) {
	s.buf.raw(s.data[l.start:l.next], l.start, 0, s.data, s.mapper, s.mapper.span(l.start, l.next))
}

// heading closes the current section and opens one at the given 1-based level.
func (s *lineSections) heading(level int, title string, span SourceSpan) {
	s.flush()
	if level < 1 {
		level = 1
	}
	// Pop by level, not depth: with levels skipped (a document that starts
	// at "=="), a later sibling must not nest under the previous one.
	for len(s.levels) > 0 && s.levels[len(s.levels)-1] >= level {
		s.path, s.levels = s.path[:len(s.path)-1], s.levels[:len(s.levels)-1]
	}
	s.path, s.levels = append(s.path, title), append(s.levels, level)
	if title != "" {
		s.headings = append(s.headings, title)
		s.headingSpans = append(s.headingSpans, span)
	}
}

func (s *lineSections) flush() {
	text, sourceMap := trimMappedText(s.buf.String(), s.buf.intervals)
	if text != "" {
		s.sections = append(s.sections, Section{HeadingPath: strings.Join(s.path, " > "),
			Text: text, Ordinal: sourceMap[0].Start, SourceMap: sourceMap})
	}
	s.buf = mappedBuilder{}
}

func (s *lineSections) finish() []Section {
	s.flush()
	if len(s.sections) == s.summaries && len(s.headings) > 0 {
		s.sections = append(s.sections, headingFallback(s.headings, s.headingSpans))
	}
	return s.sections
}

// headingFallback gives a document with headings but no body one chunk that
// carries every heading, so it stays searchable.
func headingFallback(headings []string, spans []SourceSpan) Section {
	var b strings.Builder
	var sourceMap []SourceInterval
	for i, heading := range headings {
		if i > 0 {
			start := b.Len()
			b.WriteByte('\n')
			sourceMap = append(sourceMap, SourceInterval{TextStart: start, TextEnd: start + 1, SourceSpan: sourcePointAt(spans[i-1])})
		}
		start := b.Len()
		b.WriteString(heading)
		sourceMap = append(sourceMap, SourceInterval{TextStart: start, TextEnd: b.Len(), SourceSpan: spans[i]})
	}
	return Section{Text: b.String(), Ordinal: spans[0].Start, SourceMap: sourceMap}
}
