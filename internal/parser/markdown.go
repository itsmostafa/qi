package parser

import (
	"bytes"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

func init() {
	Register(".md", &markdownParser{})
	Register(".markdown", &markdownParser{})
}

type markdownParser struct{}

func (p *markdownParser) Parse(path string, data []byte) (*Document, error) {
	meta, body, offset := splitFrontmatter(data)

	md := goldmark.New()
	reader := text.NewReader(body)
	node := md.Parser().Parse(reader)

	doc := &Document{Meta: meta, Title: meta.Title}
	var sections []Section
	var currentHeadings []string
	var currentBuf strings.Builder
	var currentOrdinal int

	// Frontmatter is worth retrieving but not worth indexing as YAML: promote it
	// to a leading section of plain prose instead of leaking syntax into chunks.
	if summary := meta.Summary(); summary != "" {
		sections = append(sections, Section{Text: summary, Ordinal: 0})
	}
	// The fallback below asks whether the body produced anything, so a
	// frontmatter summary must not count as body text.
	fromFrontmatter := len(sections)

	var firstHeading string
	var firstHeadingOrdinal int

	flush := func() {
		text := strings.TrimSpace(currentBuf.String())
		if text != "" {
			sections = append(sections, Section{
				HeadingPath: strings.Join(currentHeadings, " > "),
				Text:        text,
				Ordinal:     currentOrdinal + offset,
			})
		}
		currentBuf.Reset()
	}

	ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		switch v := n.(type) {
		case *ast.Heading:
			if entering {
				flush()
				headingText := nodeText(v, body)
				level := v.Level
				if level == 1 && doc.Title == "" {
					doc.Title = headingText
				}
				// Truncate heading stack to this level
				if level-1 < len(currentHeadings) {
					currentHeadings = currentHeadings[:level-1]
				}
				currentHeadings = append(currentHeadings, headingText)
				if v.Lines() != nil && v.Lines().Len() > 0 {
					seg := v.Lines().At(0)
					currentOrdinal = seg.Start
				}
				if firstHeading == "" {
					firstHeading, firstHeadingOrdinal = headingText, currentOrdinal
				}
			}
		case *ast.Paragraph, *ast.FencedCodeBlock, *ast.CodeBlock:
			if entering {
				currentBuf.WriteString(nodeText(v, body))
				currentBuf.WriteByte('\n')
			}
		case *ast.ListItem:
			if entering {
				// Take the whole item — including any nested list — in one pass
				// and skip children, or the *ast.Paragraph case above would
				// emit a loose item's text a second time.
				currentBuf.WriteString("- ")
				currentBuf.WriteString(nodeText(v, body))
				currentBuf.WriteByte('\n')
				return ast.WalkSkipChildren, nil
			}
		}
		return ast.WalkContinue, nil
	})

	flush()
	// A document that is nothing but headings would otherwise be stored active
	// with zero chunks and be unfindable. Headings of documents that do have
	// body text need no chunk of their own: they are already searchable through
	// chunks_fts.heading_path.
	if len(sections) == fromFrontmatter && firstHeading != "" {
		sections = append(sections, Section{Text: firstHeading, Ordinal: firstHeadingOrdinal + offset})
	}
	doc.Sections = sections
	return doc, nil
}

// nodeText returns the text of n and every descendant. Container nodes such as
// ListItem and Blockquote hold their words several levels down, so scanning
// direct children alone silently drops them.
func nodeText(n ast.Node, src []byte) string {
	var buf bytes.Buffer
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			// Keep block structure readable when several blocks flatten into
			// one string, as a nested list inside a list item does.
			if c != n && c.Type() == ast.TypeBlock {
				buf.WriteByte('\n')
			}
			return ast.WalkContinue, nil
		}
		switch t := c.(type) {
		case *ast.Text:
			buf.Write(t.Segment.Value(src))
			if t.SoftLineBreak() || t.HardLineBreak() {
				buf.WriteByte(' ')
			}
		case *ast.String:
			buf.Write(t.Value)
		case *ast.AutoLink:
			buf.Write(t.URL(src))
		case *ast.FencedCodeBlock, *ast.CodeBlock:
			lines := c.Lines()
			for i := 0; i < lines.Len(); i++ {
				seg := lines.At(i)
				buf.Write(seg.Value(src))
			}
		}
		return ast.WalkContinue, nil
	})
	return strings.TrimSpace(buf.String())
}
