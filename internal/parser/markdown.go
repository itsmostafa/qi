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
	md := goldmark.New()
	reader := text.NewReader(data)
	node := md.Parser().Parse(reader)

	doc := &Document{}
	var sections []Section
	var currentHeadings []string
	var currentBuf strings.Builder
	var currentOrdinal int

	flush := func() {
		text := strings.TrimSpace(currentBuf.String())
		if text != "" {
			sections = append(sections, Section{
				HeadingPath: strings.Join(currentHeadings, " > "),
				Text:        text,
				Ordinal:     currentOrdinal,
			})
		}
		currentBuf.Reset()
	}

	ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		switch v := n.(type) {
		case *ast.Heading:
			if entering {
				flush()
				headingText := extractText(v, data)
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
			}
		case *ast.Paragraph, *ast.FencedCodeBlock, *ast.CodeBlock, *ast.Blockquote:
			if entering {
				t := extractText(v, data)
				currentBuf.WriteString(t)
				currentBuf.WriteByte('\n')
			}
		case *ast.ListItem:
			if entering {
				t := extractText(v, data)
				currentBuf.WriteString("- ")
				currentBuf.WriteString(t)
				currentBuf.WriteByte('\n')
			}
		}
		return ast.WalkContinue, nil
	})

	flush()
	doc.Sections = sections
	return doc, nil
}

func extractText(n ast.Node, src []byte) string {
	var buf bytes.Buffer
	if n.Lines() != nil {
		for i := 0; i < n.Lines().Len(); i++ {
			seg := n.Lines().At(i)
			buf.Write(seg.Value(src))
		}
		return strings.TrimSpace(buf.String())
	}
	// Walk children for inline content
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		if t, ok := child.(*ast.Text); ok {
			buf.Write(t.Segment.Value(src))
			if t.SoftLineBreak() || t.HardLineBreak() {
				buf.WriteByte(' ')
			}
		}
	}
	return strings.TrimSpace(buf.String())
}
