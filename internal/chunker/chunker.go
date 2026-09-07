package chunker

import "github.com/itsmostafa/qi/internal/parser"

// Chunk is a unit of text to be indexed and embedded.
type Chunk struct {
	Seq         int
	Text        string
	HeadingPath string
	// Ordinal is the raw byte offset where this chunk starts. StartLine and
	// EndLine are 1-indexed inclusive raw-source lines.
	Ordinal   int
	StartLine int
	EndLine   int
}

// Chunker splits a parsed document into indexable chunks.
type Chunker interface {
	Chunk(doc *parser.Document) []Chunk
}
