package parser

// Section is a logical division of a document (heading + content).
type Section struct {
	HeadingPath string // e.g. "Introduction > Background"
	Text        string
	Ordinal     int // byte offset where this section starts
}

// Document is the parsed output of a file.
type Document struct {
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
