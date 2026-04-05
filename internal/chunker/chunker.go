package chunker

import "github.com/itsmostafa/qi/internal/parser"

// Chunk is a unit of text to be indexed and embedded.
type Chunk struct {
	Seq         int
	Text        string
	HeadingPath string
	Ordinal     int // byte offset in original document
}

// Chunker splits a parsed document into indexable chunks.
type Chunker interface {
	Chunk(doc *parser.Document) []Chunk
}
