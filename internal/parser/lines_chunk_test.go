package parser_test

import (
	"strings"
	"testing"

	"github.com/itsmostafa/qi/internal/chunker"
	"github.com/itsmostafa/qi/internal/parser"
)

// assertChunksCiteSource runs a document through the chunker at several sizes
// and checks every chunk's line range is valid and actually contains the
// chunk's text in the raw file.
func assertChunksCiteSource(t *testing.T, name, src string) {
	t.Helper()
	ext := name[strings.LastIndex(name, "."):]
	doc, err := parser.For(ext).Parse(name, []byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	raw := strings.Split(src, "\n")
	for _, size := range []int{8, 40, 512} {
		chunks := chunker.NewBreakpointChunker(size).Chunk(doc)
		if len(chunks) == 0 {
			t.Fatalf("size %d: no chunks", size)
		}
		for _, ch := range chunks {
			if ch.StartLine < 1 || ch.EndLine < ch.StartLine || ch.EndLine > len(raw) {
				t.Fatalf("size %d: invalid range %d-%d for %q", size, ch.StartLine, ch.EndLine, ch.Text)
			}
			cited := strings.Join(raw[ch.StartLine-1:ch.EndLine], "\n")
			for _, line := range strings.Split(ch.Text, "\n") {
				if line = strings.TrimSpace(line); line != "" && !strings.Contains(cited, line) {
					t.Errorf("size %d: chunk line %q not within cited lines %d-%d:\n%s", size, line, ch.StartLine, ch.EndLine, cited)
				}
			}
		}
	}
}

func TestRSTChunksCiteSourceLines(t *testing.T) {
	assertChunksCiteSource(t, "guide.rst", rstSample)
}

func TestAdocChunksCiteSourceLines(t *testing.T) {
	assertChunksCiteSource(t, "guide.adoc", adocSample)
	assertChunksCiteSource(t, "crlf.adoc", strings.ReplaceAll(adocSample, "\n", "\r\n"))
}

func TestMDXChunksCiteSourceLines(t *testing.T) {
	assertChunksCiteSource(t, "guide.mdx", mdxNoisySample)
}

const rstSample = `=============
 User Guide
=============

Welcome to the guide.

Installation
============

Run the installer::

    Not A Title
    ===========

Configure
---------

Set options here.

----

After the transition.

=====  =====
col a  col b
=====  =====
x      y
=====  =====

Usage
=====

Use it well.
`

const adocSample = `= Admin Guide
Jane Doe <jane@example.com>
v1.2, 2026-03-04
:revdate: 2026-03-04
:keywords: admin, ops
:description: How to run the service.
:toc:

Preamble text.

== Install

Install steps.

----
== not a heading
----

////
== commented heading
secret comment
////

// single line comment

=== Verify

Verify text.

## Markdown Style

Markdown heading body.
`

const mdxNoisySample = "---\ntitle: T\n---\n" +
	"import Tabs from '@theme/Tabs'\nimport {\n  A,\n  B,\n} from './x'\nexport const meta = {\n  a: 1,\n}\n\n" +
	"# Install\n\n<Tabs>\n<TabItem value=\"a\">\nhidden tab text\n</TabItem>\n</Tabs>\n\n" +
	"```jsx\n<Foo />\nimport X from 'y'\n```\n\nClosing words.\n"
