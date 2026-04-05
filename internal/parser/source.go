package parser

import (
	"path/filepath"
	"strings"
)

func init() {
	s := &sourceParser{}
	for _, ext := range []string{".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".rs", ".java", ".c", ".cpp", ".h"} {
		Register(ext, s)
	}
}

type sourceParser struct{}

func (p *sourceParser) Parse(path string, data []byte) (*Document, error) {
	text := string(data)
	title := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return &Document{
		Title: title,
		Sections: []Section{
			{Text: text, Ordinal: 0},
		},
	}, nil
}
