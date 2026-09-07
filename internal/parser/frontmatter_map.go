package parser

import (
	"bytes"

	"gopkg.in/yaml.v3"
)

// frontmatterFieldSpans maps metadata values to their YAML source ranges. It
// runs as part of frontmatter extraction, never by searching rendered prose.
func frontmatterFieldSpans(data []byte, offset int, mapper sourceMapper) map[string]SourceSpan {
	if offset == 0 || len(data) == 0 {
		return nil
	}
	open := bytes.IndexByte(data, '\n') + 1
	if open <= 0 || open > offset {
		return nil
	}
	yamlEnd := offset
	for lineStart := open; lineStart < offset; {
		lineEnd := bytes.IndexByte(data[lineStart:offset], '\n')
		if lineEnd < 0 {
			lineEnd = offset - lineStart
		}
		lineEnd += lineStart
		line := bytes.TrimSuffix(data[lineStart:lineEnd], []byte{'\r'})
		if bytes.Equal(line, []byte("---")) || bytes.Equal(line, []byte("...")) {
			yamlEnd = lineStart
			break
		}
		if lineEnd == offset {
			break
		}
		lineStart = lineEnd + 1
	}
	yamlData := data[open:yamlEnd]
	var document yaml.Node
	if err := yaml.Unmarshal(yamlData, &document); err != nil || len(document.Content) == 0 {
		return nil
	}
	root := document.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil
	}

	// Mapping values know their starting line. The next top-level key bounds a
	// block value (including a sequence or block scalar) without guessing from
	// generated text.
	type field struct {
		name      string
		keyLine   int
		valueLine int
		nextLine  int
	}
	var fields []field
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, value := root.Content[i], root.Content[i+1]
		if key.Value == "" {
			continue
		}
		line := value.Line
		if line <= 0 {
			line = key.Line
		}
		fields = append(fields, field{name: key.Value, keyLine: key.Line, valueLine: line})
	}
	lineCount := bytes.Count(yamlData, []byte{'\n'}) + 2
	for i := range fields {
		fields[i].nextLine = lineCount
		if i+1 < len(fields) && fields[i+1].keyLine > fields[i].keyLine {
			fields[i].nextLine = fields[i+1].keyLine
		}
	}

	lineStarts := []int{0}
	for i, b := range yamlData {
		if b == '\n' {
			lineStarts = append(lineStarts, i+1)
		}
	}
	lineOffset := func(line int) int {
		if line < 1 {
			line = 1
		}
		if line > len(lineStarts) {
			return len(yamlData)
		}
		return lineStarts[line-1]
	}

	out := make(map[string]SourceSpan, len(fields))
	for _, f := range fields {
		start := lineOffset(f.valueLine)
		end := lineOffset(f.nextLine)
		if end < start {
			end = start
		}
		out[f.name] = mapper.span(open+start, open+end)
	}
	return out
}
