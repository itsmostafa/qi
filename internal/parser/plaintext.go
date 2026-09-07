package parser

import (
	"path/filepath"
	"strings"
)

func init() {
	pt := &plaintextParser{}
	Register(".txt", pt)
	Register(".text", pt)
}

type plaintextParser struct{}

func (p *plaintextParser) Parse(path string, data []byte) (*Document, error) {
	text := string(data)
	title := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	mapper := newSourceMapper(data)
	return &Document{
		Title: title,
		Sections: []Section{
			{Text: text, Ordinal: 0, SourceMap: rawLineMap(data, mapper)},
		},
	}, nil
}
