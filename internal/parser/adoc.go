package parser

import (
	"cmp"
	"strings"
)

func init() {
	p := &adocParser{}
	Register(".adoc", p)
	Register(".asciidoc", p)
}

// adocParser splits AsciiDoc into sections at its section titles and keeps the
// body as source text. The document header (title, author and revision lines,
// attribute entries) becomes document metadata rather than body text.
type adocParser struct{}

// adocHeading parses an ATX-style section title: "=" or "#" repeated one to
// six times, a space, then the title. Symmetric trailing markers are dropped.
func adocHeading(line string) (int, string, bool) {
	if line == "" || (line[0] != '=' && line[0] != '#') {
		return 0, "", false
	}
	marker := line[0]
	level := 0
	for level < len(line) && line[level] == marker {
		level++
	}
	if level > 6 || level >= len(line) || (line[level] != ' ' && line[level] != '\t') {
		return 0, "", false
	}
	title := strings.TrimSpace(line[level:])
	if trimmed := strings.TrimRight(title, string(marker)); len(trimmed) < len(title) && strings.HasSuffix(trimmed, " ") {
		title = strings.TrimSpace(trimmed)
	}
	if title == "" {
		return 0, "", false
	}
	return level, title, true
}

// adocAttribute parses an attribute entry ":name: value". Unset forms
// (":name!:", ":!name:") parse with an empty value.
func adocAttribute(line string) (string, string, bool) {
	if len(line) < 3 || line[0] != ':' {
		return "", "", false
	}
	end := strings.Index(line[1:], ":")
	if end <= 0 {
		return "", "", false
	}
	name := line[1 : end+1]
	if strings.ContainsAny(name, " \t") {
		return "", "", false
	}
	value := line[end+2:]
	if value != "" && value[0] != ' ' && value[0] != '\t' {
		return "", "", false
	}
	return strings.ToLower(strings.Trim(name, "!")), strings.TrimSpace(value), true
}

func adocLineComment(line string) bool {
	return strings.HasPrefix(line, "//") && !strings.HasPrefix(line, "///")
}

// adocDelimiter reports whether a line opens a delimited block, returning the
// line that closes it. Listing, literal, example, sidebar, passthrough, quote
// and comment blocks repeat one character four or more times; open blocks use
// "--"; tables use "|==="; fenced code uses backticks.
func adocDelimiter(line string) (string, bool) {
	switch {
	case strings.HasPrefix(line, "```"):
		return "```", true
	case line == "--":
		return line, true
	case len(line) >= 4 && strings.Contains("|,:!", line[:1]) && strings.Trim(line[1:], "=") == "":
		return line, true
	case len(line) >= 4 && strings.Contains("-.=*_+/", line[:1]) && strings.Trim(line, line[:1]) == "":
		return line, true
	}
	return "", false
}

func (p *adocParser) Parse(path string, data []byte) (*Document, error) {
	lines := splitRawLines(data)
	text := func(i int) string { return strings.TrimRight(lines[i].text(data), " \t") }
	s := newLineSections(data)
	doc := &Document{}

	// Header: an optional level-0 title, then author and revision lines and
	// attribute entries up to the first blank line.
	i := 0
	for i < len(lines) && (text(i) == "" || adocLineComment(text(i))) {
		i++
	}
	attrs := map[string]rawLine{}
	values := map[string]string{}
	titled := false
	if i < len(lines) {
		if level, title, ok := adocHeading(text(i)); ok && level == 1 {
			doc.Title = title
			s.heading(1, title, s.mapper.span(lines[i].start, lines[i].end))
			titled = true
			i++
		}
	}
	for n := 0; i < len(lines) && text(i) != ""; i++ {
		t := text(i)
		if adocLineComment(t) {
			continue
		}
		// A delimited block (a //// comment, say) ends the header; taken as
		// an author line, its closing delimiter would open a block that
		// swallows the rest of the document.
		if _, ok := adocDelimiter(t); ok {
			break
		}
		name, value, ok := adocAttribute(t)
		if !ok {
			// Author and revision lines follow the title directly.
			if titled && n < 2 && len(attrs) == 0 {
				n++
				continue
			}
			break
		}
		// A trailing " \" continues the value on the next line.
		first := lines[i]
		for strings.HasSuffix(value, " \\") && i+1 < len(lines) && text(i+1) != "" {
			i++
			value = strings.TrimSpace(strings.TrimSuffix(value, "\\")) + " " + strings.TrimSpace(text(i))
		}
		attrs[name] = rawLine{start: first.start, end: lines[i].end, next: lines[i].next}
		values[name] = value
	}

	doc.Meta.Timestamp = cmp.Or(values["revdate"], values["date"])
	doc.Meta.Description = values["description"]
	tagKey := "keywords"
	if values[tagKey] == "" {
		tagKey = "tags"
	}
	for _, tag := range strings.Split(values[tagKey], ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			doc.Meta.Tags = append(doc.Meta.Tags, tag)
		}
	}
	// Description and tags stay searchable as a leading summary, as Markdown
	// frontmatter does, each mapped to its attribute entry.
	var summary mappedBuilder
	for _, field := range []struct{ key, text string }{
		{"description", doc.Meta.Description},
		{tagKey, strings.Join(doc.Meta.Tags, ", ")},
	} {
		if field.text == "" {
			continue
		}
		span := s.mapper.span(attrs[field.key].start, attrs[field.key].end)
		if summary.Len() > 0 {
			summary.fallback("\n", sourcePointAt(span))
		}
		summary.fallback(field.text, span)
	}
	if summary.Len() > 0 {
		s.sections = append(s.sections, Section{Text: summary.String(), Ordinal: summary.intervals[0].Start,
			SourceMap: summary.intervals})
		s.summaries = 1
	}

	closing, comment := "", false
	for ; i < len(lines); i++ {
		t := text(i)
		if closing != "" {
			if t == closing {
				closing = ""
			}
			if !comment {
				s.line(lines[i])
			}
			continue
		}
		if c, ok := adocDelimiter(t); ok {
			closing, comment = c, strings.HasPrefix(t, "////")
			if !comment {
				s.line(lines[i])
			}
			continue
		}
		if adocLineComment(t) {
			continue
		}
		if level, title, ok := adocHeading(t); ok {
			if level == 1 && doc.Title == "" {
				doc.Title = title
			}
			s.heading(level, title, s.mapper.span(lines[i].start, lines[i].end))
			continue
		}
		s.line(lines[i])
	}
	doc.Sections = s.finish()
	return doc, nil
}
