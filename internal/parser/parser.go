package parser

// SourceSpan identifies the raw source represented by a piece of parsed text.
// Offsets are byte offsets (end exclusive); lines are 1-indexed and inclusive.
type SourceSpan struct {
	Start     int
	End       int
	StartLine int
	EndLine   int
}

// SourceInterval maps a transformed text byte interval to a raw source span.
// Keeping intervals rather than one entry per rune bounds map memory by AST
// fragments and source lines while retaining exact literal byte deltas. For
// non-1:1 generated text, the source span is a conservative envelope.
type SourceInterval struct {
	TextStart int
	TextEnd   int
	// Literal intervals preserve bytes exactly and lie within one source line.
	Literal bool
	SourceSpan
}

// Section is a logical division of a document (heading + content).
type Section struct {
	HeadingPath string // e.g. "Introduction > Background"
	Text        string
	Ordinal     int // byte offset where this section starts

	// SourceMap maps transformed byte intervals to raw source spans.
	SourceMap []SourceInterval
}

// Document is the parsed output of a file.
type Document struct {
	Meta     Meta
	Title    string
	Sections []Section
}

// Parser extracts structure from a file's bytes.
type Parser interface {
	Parse(path string, data []byte) (*Document, error)
}

// registry maps file extensions to Parsers.
var registry = map[string]Parser{}

// Register associates an extension (e.g. ".md") with a Parser.
func Register(ext string, p Parser) {
	registry[ext] = p
}

// For returns the Parser for the given extension, or plaintext fallback.
func For(ext string) Parser {
	if p, ok := registry[ext]; ok {
		return p
	}
	return registry[".txt"]
}
