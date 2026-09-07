package parser

import (
	"bytes"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

func init() {
	Register(".md", &markdownParser{})
	Register(".markdown", &markdownParser{})
}

type markdownParser struct{}

type mappedText struct {
	text      string
	intervals []SourceInterval
}

type mappedBuilder struct {
	strings.Builder
	intervals []SourceInterval
}

func (b *mappedBuilder) fallback(value string, span SourceSpan) {
	start := b.Len()
	b.WriteString(value)
	if len(value) > 0 {
		b.intervals = append(b.intervals, SourceInterval{TextStart: start, TextEnd: b.Len(), SourceSpan: span})
	}
}

func (b *mappedBuilder) raw(value []byte, rawStart, rawBase int, src []byte, mapper sourceMapper, fallback SourceSpan) {
	if rawStart < 0 || rawStart+len(value) > len(src) || !bytes.Equal(src[rawStart:rawStart+len(value)], value) {
		b.fallback(string(value), fallback)
		return
	}
	textStart := b.Len()
	b.Write(value)
	for from := 0; from < len(value); {
		to := len(value)
		if i := bytes.IndexByte(value[from:], '\n'); i >= 0 {
			to = from + i + 1
		}
		b.intervals = append(b.intervals, SourceInterval{TextStart: textStart + from, TextEnd: textStart + to,
			Literal: true, SourceSpan: mapper.span(rawBase+rawStart+from, rawBase+rawStart+to)})
		from = to
	}
}

func (p *markdownParser) Parse(path string, data []byte) (*Document, error) {
	meta, body, offset := splitFrontmatter(data)
	mapper := newSourceMapper(data)
	md := goldmark.New()
	node := md.Parser().Parse(text.NewReader(body))

	doc := &Document{Meta: meta, Title: meta.Title}
	var sections []Section
	var currentHeadings []string
	var currentBuf strings.Builder
	var currentMap []SourceInterval

	if summaryFields := meta.summaryFields(); len(summaryFields) > 0 {
		parts := make([]string, len(summaryFields))
		for i, field := range summaryFields {
			parts[i] = field.text
		}
		summary := strings.Join(parts, "\n")
		whole := mapper.span(0, offset)
		fields := frontmatterFieldSpans(data, offset, mapper)
		var summaryMap []SourceInterval
		textOffset := 0
		for i, field := range summaryFields {
			if i > 0 {
				separator := SourceInterval{TextStart: textOffset, TextEnd: textOffset + 1, SourceSpan: sourcePointAt(summaryMap[len(summaryMap)-1].SourceSpan)}
				summaryMap = append(summaryMap, separator)
				textOffset++
			}
			span, ok := fields[field.key]
			if !ok {
				span = whole
			}
			summaryMap = append(summaryMap, SourceInterval{TextStart: textOffset, TextEnd: textOffset + len(field.text), SourceSpan: span})
			textOffset += len(field.text)
		}
		sections = append(sections, Section{Text: summary, Ordinal: summaryMap[0].SourceSpan.Start,
			SourceMap: summaryMap})
	}
	fromFrontmatter := len(sections)
	var headings []string
	var headingSpans []SourceSpan

	flush := func() {
		text, sourceMap := trimMappedText(currentBuf.String(), currentMap)
		if text != "" {
			ordinal := 0
			if len(sourceMap) > 0 {
				ordinal = sourceMap[0].SourceSpan.Start
			}
			sections = append(sections, Section{HeadingPath: strings.Join(currentHeadings, " > "),
				Text: text, Ordinal: ordinal, SourceMap: sourceMap})
		}
		currentBuf.Reset()
		currentMap = nil
	}

	appendMapped := func(n ast.Node, mapped mappedText) {
		if mapped.text == "" {
			return
		}
		base := currentBuf.Len()
		if base > 0 {
			separator := sourcePoint(n, offset, mapper)
			currentMap = append(currentMap, SourceInterval{TextStart: base, TextEnd: base + 1, SourceSpan: separator})
			currentBuf.WriteByte('\n')
			base++
		}
		currentBuf.WriteString(mapped.text)
		for _, interval := range mapped.intervals {
			interval.TextStart += base
			interval.TextEnd += base
			currentMap = append(currentMap, interval)
		}
	}

	ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		switch v := n.(type) {
		case *ast.Heading:
			if entering {
				flush()
				headingText := nodeMappedText(v, body, offset, mapper).text
				if v.Level == 1 && doc.Title == "" {
					doc.Title = headingText
				}
				if v.Level-1 < len(currentHeadings) {
					currentHeadings = currentHeadings[:v.Level-1]
				}
				currentHeadings = append(currentHeadings, headingText)
				if headingText != "" {
					headings = append(headings, headingText)
					headingSpans = append(headingSpans, nodeSourceSpan(v, offset, mapper))
				}
			}
		case *ast.Paragraph, *ast.FencedCodeBlock, *ast.CodeBlock:
			if entering {
				appendMapped(v, nodeMappedText(v, body, offset, mapper))
			}
		case *ast.ListItem:
			if entering {
				mapped := nodeMappedText(v, body, offset, mapper)
				if mapped.text != "" {
					span := sourcePoint(v, offset, mapper)
					mapped.text = "- " + mapped.text
					mapped.intervals = prependSynthetic(mapped.intervals, span, 2)
					appendMapped(v, mapped)
				}
				return ast.WalkSkipChildren, nil
			}
		}
		return ast.WalkContinue, nil
	})

	flush()
	if len(sections) == fromFrontmatter && len(headings) > 0 {
		var b strings.Builder
		var sourceMap []SourceInterval
		for i, heading := range headings {
			if i > 0 {
				start := b.Len()
				b.WriteByte('\n')
				sourceMap = append(sourceMap, SourceInterval{TextStart: start, TextEnd: start + 1, SourceSpan: sourcePointAt(headingSpans[i-1])})
			}
			start := b.Len()
			b.WriteString(heading)
			sourceMap = append(sourceMap, SourceInterval{TextStart: start, TextEnd: b.Len(), SourceSpan: headingSpans[i]})
		}
		text := b.String()
		sections = append(sections, Section{Text: text, Ordinal: headingSpans[0].Start,
			SourceMap: sourceMap})
	}
	doc.Sections = sections
	return doc, nil
}

// nodeMappedText emits retrieval text and its raw-source map together, rather
// than trying to locate transformed text in the source afterward.
func nodeMappedText(n ast.Node, src []byte, offset int, mapper sourceMapper) mappedText {
	var b mappedBuilder
	fallback := nodeSourceSpan(n, offset, mapper)
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			if c != n && c.Type() == ast.TypeBlock {
				b.fallback("\n", sourcePoint(c, offset, mapper))
			}
			return ast.WalkContinue, nil
		}
		switch t := c.(type) {
		case *ast.Text:
			seg := t.Segment
			value := seg.Value(src)
			b.raw(value, seg.Start, offset, src, mapper, mapper.span(offset+seg.Start, offset+seg.Stop))
			if t.SoftLineBreak() || t.HardLineBreak() {
				// The source newline belongs to this line, not the following one.
				b.fallback(" ", mapper.span(offset+seg.Stop, offset+seg.Stop))
			}
		case *ast.String:
			b.fallback(string(t.Value), fallback)
		case *ast.AutoLink:
			b.fallback(string(t.URL(src)), fallback)
		case *ast.FencedCodeBlock, *ast.CodeBlock:
			lines := c.Lines()
			for i := 0; i < lines.Len(); i++ {
				seg := lines.At(i)
				b.raw(seg.Value(src), seg.Start, offset, src, mapper, mapper.span(offset+seg.Start, offset+seg.Stop))
			}
		}
		return ast.WalkContinue, nil
	})
	text, intervals := trimMappedText(b.String(), b.intervals)
	return mappedText{text: text, intervals: intervals}
}

func trimMappedText(text string, intervals []SourceInterval) (string, []SourceInterval) {
	left := strings.TrimLeftFunc(text, unicode.IsSpace)
	start := len(text) - len(left)
	end := start + len(strings.TrimRightFunc(left, unicode.IsSpace))
	out := make([]SourceInterval, 0, len(intervals))
	for _, interval := range intervals {
		if interval.TextEnd <= start || interval.TextStart >= end {
			continue
		}
		interval = interval.Clip(start, end)
		interval.TextStart -= start
		interval.TextEnd -= start
		out = append(out, interval)
	}
	return text[start:end], out
}

func prependSynthetic(intervals []SourceInterval, span SourceSpan, prefixBytes int) []SourceInterval {
	out := []SourceInterval{{TextStart: 0, TextEnd: prefixBytes, SourceSpan: span}}
	for _, interval := range intervals {
		interval.TextStart += prefixBytes
		interval.TextEnd += prefixBytes
		out = append(out, interval)
	}
	return out
}

func sourcePoint(n ast.Node, offset int, mapper sourceMapper) SourceSpan {
	span := nodeSourceSpan(n, offset, mapper)
	return sourcePointAt(span)
}

func sourcePointAt(span SourceSpan) SourceSpan {
	span.End = span.Start
	span.EndLine = span.StartLine
	return span
}

func nodeSourceSpan(n ast.Node, offset int, mapper sourceMapper) SourceSpan {
	start, end := -1, -1
	consider := func(v ast.Node) {
		if v.Type() != ast.TypeBlock {
			return
		}
		lines := v.Lines()
		if lines == nil || lines.Len() == 0 {
			return
		}
		first, last := lines.At(0), lines.At(lines.Len()-1)
		if start < 0 || first.Start < start {
			start = first.Start
		}
		if last.Stop > end {
			end = last.Stop
		}
	}
	consider(n)
	if start < 0 {
		_ = ast.Walk(n, func(v ast.Node, entering bool) (ast.WalkStatus, error) {
			if entering {
				consider(v)
			}
			return ast.WalkContinue, nil
		})
	}
	if start < 0 {
		return mapper.span(offset, offset)
	}
	return mapper.span(offset+start, offset+end)
}
