package parser

import (
	"bytes"
	"strings"

	"gopkg.in/yaml.v3"
)

// Meta holds YAML frontmatter fields promoted to document level.
type Meta struct {
	Title       string `yaml:"title"`
	Description string `yaml:"description"`
	Timestamp   string `yaml:"timestamp"`
	// Date is the documented alias for Timestamp; read it via Timestamp.
	Date string     `yaml:"date"`
	Tags stringList `yaml:"tags"`
}

// Summary renders the retrieval-worthy frontmatter as plain prose. It is empty
// when there is nothing worth indexing.
func (m Meta) Summary() string {
	var parts []string
	for _, s := range []string{m.Title, m.Description, strings.Join(m.Tags, ", ")} {
		if s = strings.TrimSpace(s); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n")
}

// stringList accepts either a YAML sequence or a comma-separated scalar, both of
// which appear in the wild for "tags".
type stringList []string

func (s *stringList) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.SequenceNode:
		var out []string
		if err := n.Decode(&out); err != nil {
			return err
		}
		*s = out
	case yaml.ScalarNode:
		for _, p := range strings.Split(n.Value, ",") {
			if p = strings.TrimSpace(p); p != "" {
				*s = append(*s, p)
			}
		}
	}
	return nil
}

// splitFrontmatter separates a leading YAML frontmatter block from the body.
// The third return is the byte length of the stripped prefix, so section
// ordinals still refer to positions in the original file. A missing, malformed
// or unterminated block is treated as ordinary content.
func splitFrontmatter(data []byte) (Meta, []byte, int) {
	var meta Meta
	if !bytes.HasPrefix(data, []byte("---\n")) && !bytes.HasPrefix(data, []byte("---\r\n")) {
		return meta, data, 0
	}

	open := bytes.IndexByte(data, '\n') + 1
	rest := data[open:]

	for off := 0; off <= len(rest); {
		next := len(rest)
		line := rest[off:]
		if i := bytes.IndexByte(rest[off:], '\n'); i >= 0 {
			line, next = rest[off:off+i], off+i+1
		}
		switch strings.TrimRight(string(line), "\r") {
		case "---", "...":
			// A closed block is frontmatter whether or not it decodes. YAML we
			// cannot read yields no metadata, but indexing it as prose would
			// put the raw block — keys, secrets and all — back into the chunk.
			if err := yaml.Unmarshal(rest[:off], &meta); err != nil {
				meta = Meta{}
			}
			if meta.Timestamp == "" {
				meta.Timestamp = meta.Date
			}
			return meta, rest[next:], open + next
		}
		if next == len(rest) && off == next {
			break
		}
		off = next
	}
	return Meta{}, data, 0 // unterminated
}
