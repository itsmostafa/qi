package parser

import (
	"strings"
	"unicode/utf8"
)

func init() {
	Register(".rst", &rstParser{})
}

// rstParser splits reStructuredText into sections at its section titles and
// keeps the body as source text. It recognises titles only; it does not render
// or interpret any other markup.
type rstParser struct{}

// rstAdornmentChars are the punctuation characters docutils accepts in
// section title adornments.
const rstAdornmentChars = "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~"

// rstAdornment reports whether a line is a section adornment: one repeated
// punctuation character starting in column 0, with nothing else but trailing
// whitespace. Internal spaces (simple-table rules like "=== ===") and mixed
// characters (grid-table borders like "+---+") do not qualify.
func rstAdornment(line string) (byte, int, bool) {
	line = strings.TrimRight(line, " \t")
	if line == "" || !strings.ContainsRune(rstAdornmentChars, rune(line[0])) {
		return 0, 0, false
	}
	c := line[0]
	for i := 1; i < len(line); i++ {
		if line[i] != c {
			return 0, 0, false
		}
	}
	return c, len(line), true
}

func rstIndented(line string) bool {
	return line != "" && (line[0] == ' ' || line[0] == '\t')
}

func isBlank(line string) bool { return strings.TrimSpace(line) == "" }

// rstTitle reports whether line can be the text of a section title whose
// adornment is width characters long.
func rstTitle(line string, width int) (string, bool) {
	title := strings.TrimSpace(line)
	if title == "" {
		return "", false
	}
	if _, _, ok := rstAdornment(title); ok {
		return "", false
	}
	return title, utf8.RuneCountInString(title) <= width
}

func (p *rstParser) Parse(path string, data []byte) (*Document, error) {
	lines := splitRawLines(data)
	text := func(i int) string { return lines[i].text(data) }
	s := newLineSections(data)
	doc := &Document{}

	// Heading levels follow the order in which adornment styles first appear.
	type style struct {
		char     byte
		overline bool
	}
	levels := map[style]int{}
	addHeading := func(st style, title string, first, last int) {
		level, ok := levels[st]
		if !ok {
			level = len(levels) + 1
			levels[st] = level
		}
		if level == 1 && doc.Title == "" {
			doc.Title = title
		}
		s.heading(level, title, s.mapper.span(lines[first].start, lines[last].end))
	}

	prevBlank := true
	literalPending, inLiteral := false, false
	for i := 0; i < len(lines); {
		t := text(i)
		blank := isBlank(t)

		// A paragraph ending in "::" introduces an indented literal block; its
		// lines are body text, never titles.
		if literalPending && !blank {
			inLiteral = rstIndented(t)
			literalPending = false
		}
		if inLiteral {
			if blank || rstIndented(t) {
				s.line(lines[i])
				i++
				continue
			}
			inLiteral = false
		}

		if prevBlank && !blank {
			// Overline and underline: the title may be indented, the two
			// adornments must match.
			if c, width, ok := rstAdornment(t); ok && i+2 < len(lines) {
				if title, ok := rstTitle(text(i+1), width); ok {
					if c2, width2, ok := rstAdornment(text(i + 2)); ok && c2 == c && width2 == width {
						addHeading(style{c, true}, title, i, i+2)
						i += 3
						prevBlank = true
						continue
					}
				}
			}
			// Underline only: the title starts in column 0.
			if !rstIndented(t) && i+1 < len(lines) {
				if c, width, ok := rstAdornment(text(i + 1)); ok {
					if title, ok := rstTitle(t, width); ok {
						addHeading(style{c, false}, title, i, i+1)
						i += 2
						prevBlank = true
						continue
					}
				}
			}
		}

		s.line(lines[i])
		if strings.HasSuffix(strings.TrimSpace(t), "::") {
			literalPending = true
		}
		prevBlank = blank
		i++
	}
	doc.Sections = s.finish()
	return doc, nil
}
