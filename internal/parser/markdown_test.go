package parser

import (
	"strings"
	"testing"
)

func parseMD(t *testing.T, src string) *Document {
	t.Helper()
	doc, err := (&markdownParser{}).Parse("note.md", []byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return doc
}

func bodyText(doc *Document) string {
	var b strings.Builder
	for _, s := range doc.Sections {
		b.WriteString(s.Text)
		b.WriteByte('\n')
	}
	return b.String()
}

// The repro from docs/audit-2026-09-03.md:380 — list text was dropped entirely,
// leaving bare "-" markers.
func TestListItemTextIsIndexed(t *testing.T) {
	doc := parseMD(t, "# List\n\n- alpha uniqueitem\n- beta seconditem\n")
	got := bodyText(doc)
	for _, want := range []string{"alpha uniqueitem", "beta seconditem"} {
		if !strings.Contains(got, want) {
			t.Errorf("list text %q missing from sections:\n%s", want, got)
		}
	}
}

func TestLooseListItemNotDoubleCounted(t *testing.T) {
	doc := parseMD(t, "# L\n\n- alpha\n\n- beta\n")
	if n := strings.Count(bodyText(doc), "alpha"); n != 1 {
		t.Errorf("loose list item emitted %d times, want 1:\n%s", n, bodyText(doc))
	}
}

func TestNestedListTextIsIndexed(t *testing.T) {
	doc := parseMD(t, "# L\n\n- outer\n    - innermost deepvalue\n")
	got := bodyText(doc)
	if !strings.Contains(got, "innermost deepvalue") {
		t.Errorf("nested list text missing:\n%s", got)
	}
	if n := strings.Count(got, "outer"); n != 1 {
		t.Errorf("nested parent emitted %d times, want 1:\n%s", n, got)
	}
}

func TestBlockquoteTextIsIndexed(t *testing.T) {
	doc := parseMD(t, "# Q\n\n> quoted uniqueword\n")
	if got := bodyText(doc); !strings.Contains(got, "quoted uniqueword") {
		t.Errorf("blockquote text missing:\n%s", got)
	}
}

const withFrontmatter = `---
type: Biomarker Panel
title: Thyroid Panel
description: TSH and thyroid antibodies.
resource: raw/biomarkers-export.csv
tags:
- biomarker
- thyroid
timestamp: 2026-07-17
---

# Overview

Body text here.
`

func TestFrontmatterTitleBeatsHeading(t *testing.T) {
	doc := parseMD(t, withFrontmatter)
	if doc.Title != "Thyroid Panel" {
		t.Errorf("Title = %q, want %q", doc.Title, "Thyroid Panel")
	}
	if doc.Meta.Timestamp != "2026-07-17" {
		t.Errorf("Timestamp = %q, want 2026-07-17", doc.Meta.Timestamp)
	}
	if len(doc.Meta.Tags) != 2 || doc.Meta.Tags[0] != "biomarker" {
		t.Errorf("Tags = %v", doc.Meta.Tags)
	}
}

// "date" is the documented alias for "timestamp"; without it these documents
// index as undated and drop out of --since/--until and date sorting.
func TestFrontmatterDateAliasesTimestamp(t *testing.T) {
	doc := parseMD(t, "---\ntitle: T\ndate: 2026-07-17\n---\n\nBody.\n")
	if doc.Meta.Timestamp != "2026-07-17" {
		t.Errorf("Timestamp = %q, want 2026-07-17", doc.Meta.Timestamp)
	}
}

// An explicit "timestamp" wins over "date".
func TestFrontmatterTimestampBeatsDate(t *testing.T) {
	doc := parseMD(t, "---\ntimestamp: 2026-01-02\ndate: 2026-07-17\n---\n\nBody.\n")
	if doc.Meta.Timestamp != "2026-01-02" {
		t.Errorf("Timestamp = %q, want 2026-01-02", doc.Meta.Timestamp)
	}
}

func TestFrontmatterScalarRangeStopsBeforeNextKey(t *testing.T) {
	doc := parseMD(t, "---\ntitle: hello\ntags:\n  - foo\n---\n\nbody\n")
	spans := sourceLineSpans(t, doc.Sections[0])
	if len(spans) != 2 {
		t.Fatalf("summary spans = %+v", spans)
	}
	if spans[0].StartLine != 2 || spans[0].EndLine != 2 {
		t.Errorf("title span = %+v, want scalar line 2 only", spans[0])
	}
	if spans[1].StartLine != 4 || spans[1].EndLine != 4 {
		t.Errorf("tags span = %+v, want value line 4", spans[1])
	}
}

func TestFrontmatterSummaryUsesFieldRanges(t *testing.T) {
	doc := parseMD(t, withFrontmatter)
	spans := sourceLineSpans(t, doc.Sections[0])
	if len(spans) != 3 {
		t.Fatalf("summary spans = %+v", spans)
	}
	if spans[0].StartLine != 3 || spans[0].EndLine != 3 {
		t.Errorf("title span = %+v, want line 3", spans[0])
	}
	if spans[1].StartLine != 4 || spans[1].EndLine != 4 {
		t.Errorf("description span = %+v, want line 4", spans[1])
	}
	if spans[2].StartLine != 7 || spans[2].EndLine != 8 {
		t.Errorf("tags span = %+v, want lines 7-8", spans[2])
	}
}

func TestFrontmatterNeverReachesChunkBody(t *testing.T) {
	got := bodyText(parseMD(t, withFrontmatter))
	for _, leak := range []string{"resource:", "type:", "timestamp:", "tags:"} {
		if strings.Contains(got, leak) {
			t.Errorf("frontmatter key %q leaked into sections:\n%s", leak, got)
		}
	}
	if strings.Contains(got, "\n- \n") || strings.HasPrefix(got, "- \n") {
		t.Errorf("bare list markers left behind:\n%q", got)
	}
	// The useful fields survive as prose so they stay searchable.
	for _, want := range []string{"Thyroid Panel", "thyroid antibodies", "biomarker"} {
		if !strings.Contains(got, want) {
			t.Errorf("frontmatter value %q not indexed:\n%s", want, got)
		}
	}
}

func TestHeadingFallbackWithoutFrontmatterTitle(t *testing.T) {
	doc := parseMD(t, "---\ntags: a, b\n---\n\n# Real Heading\n\ntext\n")
	if doc.Title != "Real Heading" {
		t.Errorf("Title = %q, want fallback to H1", doc.Title)
	}
	if len(doc.Meta.Tags) != 2 {
		t.Errorf("scalar tags = %v, want 2 entries", doc.Meta.Tags)
	}
}

func TestOrdinalsReferToOriginalFile(t *testing.T) {
	doc := parseMD(t, withFrontmatter)
	want := strings.Index(withFrontmatter, "# Overview")
	for _, s := range doc.Sections {
		if s.HeadingPath == "Overview" {
			if s.Ordinal < want {
				t.Errorf("Ordinal = %d, want >= %d (offset not restored)", s.Ordinal, want)
			}
			return
		}
	}
	t.Fatal("no Overview section")
}

func TestMalformedFrontmatterIsTreatedAsContent(t *testing.T) {
	for name, src := range map[string]string{
		"unterminated": "---\ntitle: X\n\n# Heading\n\ntext\n",
		"notYAML":      "---\n\tthis: [is: broken\n---\n\n# Heading\n\ntext\n",
		"none":         "# Heading\n\ntext\n",
	} {
		doc := parseMD(t, src)
		if !strings.Contains(bodyText(doc), "text") {
			t.Errorf("%s: body lost:\n%s", name, bodyText(doc))
		}
	}
}

// A plain []string cannot decode `tags: personal`, and an unmarshal error used
// to un-strip the block, putting the raw YAML back into the chunk.
func TestScalarTagsDoNotLeakFrontmatter(t *testing.T) {
	doc := parseMD(t, "---\ntitle: T\ntags: personal\n---\n\n# H\n\nbody\n")
	if len(doc.Meta.Tags) != 1 || doc.Meta.Tags[0] != "personal" {
		t.Errorf("Tags = %v, want [personal]", doc.Meta.Tags)
	}
	if got := bodyText(doc); strings.Contains(got, "tags:") {
		t.Errorf("scalar tags leaked frontmatter into the body:\n%s", got)
	}
}

func TestFlowSequenceTags(t *testing.T) {
	doc := parseMD(t, "---\ntags: [book, summary]\n---\n\n# H\n\nbody\n")
	if len(doc.Meta.Tags) != 2 {
		t.Errorf("flow sequence tags = %v, want 2 entries", doc.Meta.Tags)
	}
}

// Delimiters, not decodability, decide what is frontmatter. YAML that does not
// fit Meta yields no metadata but must still be stripped, or the raw block —
// keys, secrets and all — reaches the index. A tag list keeps the closing ---
// from being read as a setext underline, so the leak lands in chunk text.
func TestUndecodableFrontmatterIsStillStripped(t *testing.T) {
	doc := parseMD(t, "---\ntitle: [bad]\nsecret: hunter2\ntags:\n- a\n---\n\n# Heading\n\nreal body\n")
	for _, s := range doc.Sections {
		if strings.Contains(s.Text, "hunter2") || strings.Contains(s.HeadingPath, "hunter2") {
			t.Fatalf("undecodable frontmatter leaked into the index: %q / %q", s.HeadingPath, s.Text)
		}
	}
	if !strings.Contains(bodyText(doc), "real body") {
		t.Error("body lost")
	}
}

// docs/audit-2026-09-03.md:413 — a heading-only file indexed zero chunks, so
// `qi search Unicorn` found nothing.
func TestHeadingOnlyDocumentIsSearchable(t *testing.T) {
	doc := parseMD(t, "# Unicorn Project\n")
	if len(doc.Sections) != 1 {
		t.Fatalf("got %d sections, want 1: %+v", len(doc.Sections), doc.Sections)
	}
	if doc.Sections[0].Text != "Unicorn Project" {
		t.Errorf("Text = %q, want %q", doc.Sections[0].Text, "Unicorn Project")
	}
}

// The fallback must not duplicate the heading into sections that have a body.
func TestHeadingNotPrependedToBody(t *testing.T) {
	doc := parseMD(t, "# Zebra\n\nbody\n")
	if len(doc.Sections) != 1 || doc.Sections[0].Text != "body" {
		t.Errorf("sections = %+v, want one section with text %q", doc.Sections, "body")
	}
}

// Empty headings in a document that has body text somewhere get no chunk of
// their own: chunks_fts indexes heading_path, so they are already searchable.
func TestEmptyHeadingsGetNoChunkWhenTheDocumentHasBody(t *testing.T) {
	doc := parseMD(t, "# A\n\n## B\n\nbody b\n\n## C\n\n### D\n\nbody d\n")
	want := []string{"body b", "body d"}
	if len(doc.Sections) != len(want) {
		t.Fatalf("got %d sections, want %d: %+v", len(doc.Sections), len(want), doc.Sections)
	}
	for i, w := range want {
		if doc.Sections[i].Text != w {
			t.Errorf("section %d text = %q, want %q", i, doc.Sections[i].Text, w)
		}
	}
	if doc.Sections[0].HeadingPath != "A > B" {
		t.Errorf("HeadingPath = %q, want %q", doc.Sections[0].HeadingPath, "A > B")
	}
}

// A document whose headings are all empty still needs one chunk, or it is
// indexed active with nothing searchable — and that chunk must carry every
// heading, not just the first, or `qi search Unicorn` misses the document whose
// only distinguishing word is in a deeper heading.
func TestHeadingOnlyDocumentIndexesEveryHeading(t *testing.T) {
	doc := parseMD(t, "# Overview\n\n## Deployment Unicorn\n")
	if len(doc.Sections) != 1 {
		t.Fatalf("got %d sections, want 1: %+v", len(doc.Sections), doc.Sections)
	}
	if got := doc.Sections[0].Text; got != "Overview\nDeployment Unicorn" {
		t.Errorf("Text = %q, want every heading", got)
	}
}

// An empty heading contributes nothing to search, so it must not pad the
// fallback chunk with blank lines.
func TestHeadingFallbackSkipsEmptyHeadings(t *testing.T) {
	doc := parseMD(t, "#\n\n## B\n")
	if len(doc.Sections) != 1 || doc.Sections[0].Text != "B" {
		t.Fatalf("sections = %+v, want one section with text %q", doc.Sections, "B")
	}
}

// Frontmatter promoted to a summary section must not count as body text, or a
// frontmatter + heading-only document loses its heading entirely.
func TestFrontmatterDoesNotSuppressTheHeadingFallback(t *testing.T) {
	doc := parseMD(t, "---\ntitle: Foo\n---\n\n# Project Unicorn\n")
	var joined string
	for _, s := range doc.Sections {
		joined += s.Text + "\n"
	}
	if !strings.Contains(joined, "Project Unicorn") {
		t.Errorf("sections = %+v, want one containing %q", doc.Sections, "Project Unicorn")
	}
}

func TestSourceSpansRemainRawWithFrontmatterCRLFAndUnicode(t *testing.T) {
	src := "---\r\ntitle: Café\r\n---\r\n\r\n# Héading\r\n\r\n- first café\r\n- second\r\n\r\n## Next\r\n\r\nbody\r\n"
	doc := parseMD(t, src)
	if len(doc.Sections) != 3 {
		t.Fatalf("got %d sections: %+v", len(doc.Sections), doc.Sections)
	}
	if got := doc.Sections[0].SourceMap[0].StartLine; got != 2 {
		t.Errorf("frontmatter title StartLine = %d, want 2", got)
	}
	for _, section := range doc.Sections[1:] {
		if len(section.SourceMap) == 0 {
			t.Fatalf("section %q has no source map", section.HeadingPath)
		}
		if section.Ordinal != section.SourceMap[0].Start {
			t.Errorf("section %q Ordinal = %d, want source start %d", section.HeadingPath, section.Ordinal, section.SourceMap[0].Start)
		}
		for _, span := range section.SourceMap {
			if span.StartLine < 6 || span.EndLine < span.StartLine {
				t.Errorf("section %q has invalid raw span %+v", section.HeadingPath, span)
			}
		}
	}
	if got := doc.Sections[1].Text; !strings.Contains(got, "first café") {
		t.Errorf("list transformation lost Unicode text: %q", got)
	}
	if got := sourceLineSpans(t, doc.Sections[1])[0].StartLine; got != 7 {
		t.Errorf("first list item source line = %d, want 7", got)
	}
	if got := sourceLineSpans(t, doc.Sections[1])[1].StartLine; got != 8 {
		t.Errorf("second list item source line = %d, want 8", got)
	}
}

func TestSourceMapUsesIntervalsNotPerRuneEntries(t *testing.T) {
	doc := parseMD(t, "# H\n\n"+strings.Repeat("x", 10000)+"\n")
	if len(doc.Sections) != 1 || len(doc.Sections[0].SourceMap) >= len([]rune(doc.Sections[0].Text))/10 {
		t.Fatalf("source map is not compressed: text=%d intervals=%d", len(doc.Sections[0].Text), len(doc.Sections[0].SourceMap))
	}
}

func sourceLineSpans(t *testing.T, section Section) []SourceSpan {
	t.Helper()
	var spans []SourceSpan
	start := 0
	for _, line := range strings.Split(section.Text, "\n") {
		span, ok := SourceRange(section.SourceMap, start, start+len(line))
		if !ok {
			t.Fatalf("no source range for %q", line)
		}
		spans = append(spans, span)
		start += len(line) + 1
	}
	return spans
}

func TestCodeLinesKeepDistinctRawSpans(t *testing.T) {
	doc := parseMD(t, "# Code\n\n```go\nα\nβ\n```\n")
	spans := sourceLineSpans(t, doc.Sections[0])
	if len(doc.Sections) != 1 || len(spans) != 2 {
		t.Fatalf("sections = %+v, want two code-line spans", doc.Sections)
	}
	if spans[0].StartLine != 4 || spans[1].StartLine != 5 {
		t.Errorf("code spans = %+v, want lines 4 and 5", spans)
	}
	if spans[0].Start >= spans[1].Start {
		t.Errorf("code spans are not ordered: %+v", spans)
	}
}

const mdxSample = "---\ntitle: Install Guide\ndate: 2026-05-01\ntags: [docs, setup]\n---\n" +
	"import Tabs from '@theme/Tabs'\nimport { Callout } from '../components'\n\n" +
	"# Install\n\nIntro para.\n\n## Platforms\n\n" +
	"<Tabs>\n<TabItem value=\"mac\">\n\nUse brew install on macos.\n\n</TabItem>\n</Tabs>\n\n" +
	"<Callout type=\"warn\" />\n\n<Callout type=\"info\">Inline callout text.</Callout>\n\n" +
	"### Verify\n\nRun qi doctor.\n"

// MDX is Markdown plus ESM and JSX; goldmark reads the ESM lines as a
// preamble paragraph and JSX tags as HTML, so the Markdown structure survives.
func TestMDXParsesAsMarkdown(t *testing.T) {
	if _, ok := For(".mdx").(*markdownParser); !ok {
		t.Fatalf("For(.mdx) = %T, want *markdownParser", For(".mdx"))
	}
	doc, err := For(".mdx").Parse("guide.mdx", []byte(mdxSample))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if doc.Title != "Install Guide" {
		t.Errorf("Title = %q", doc.Title)
	}
	if doc.Meta.Timestamp != "2026-05-01" {
		t.Errorf("Timestamp = %q", doc.Meta.Timestamp)
	}
	if len(doc.Meta.Tags) != 2 || doc.Meta.Tags[0] != "docs" || doc.Meta.Tags[1] != "setup" {
		t.Errorf("Tags = %v", doc.Meta.Tags)
	}

	paths := map[string]string{}
	for _, s := range doc.Sections {
		paths[s.HeadingPath] += s.Text
		for _, iv := range s.SourceMap {
			if iv.StartLine <= 0 || iv.EndLine < iv.StartLine {
				t.Errorf("section %q has invalid line range %d-%d", s.HeadingPath, iv.StartLine, iv.EndLine)
			}
		}
	}
	for _, want := range []string{"Install", "Install > Platforms", "Install > Platforms > Verify"} {
		if _, ok := paths[want]; !ok {
			t.Errorf("heading path %q missing; have %v", want, paths)
		}
	}
	for _, want := range []string{"Use brew install on macos.", "Inline callout text."} {
		if !strings.Contains(paths["Install > Platforms"], want) {
			t.Errorf("JSX child text %q missing from Platforms section:\n%s", want, paths["Install > Platforms"])
		}
	}
	if strings.Contains(paths["Install"], "import") {
		t.Errorf("import line leaked into a headed section:\n%s", paths["Install"])
	}
}

func TestMDXDropsESMAndIndexesJSXChildren(t *testing.T) {
	doc := parseWith(t, "guide.mdx", mdxSample+"\n<Tabs>\n<TabItem value=\"b\">\ntight child text\n</TabItem>\n</Tabs>\n")
	body := bodyText(doc)
	for _, leak := range []string{"import", "@theme/Tabs", "<TabItem", "</Tabs>", "<Callout type=\"warn\" />"} {
		if strings.Contains(body, leak) {
			t.Errorf("MDX noise %q indexed:\n%s", leak, body)
		}
	}
	for _, s := range doc.Sections {
		if s.HeadingPath == "" && s.Text != "Install Guide\ndocs, setup" {
			t.Errorf("untitled non-frontmatter section: %q", s.Text)
		}
	}
	got := sectionsByPath(doc)["Install > Platforms > Verify"]
	if !strings.Contains(got, "tight child text") {
		t.Errorf("JSX child without blank lines not indexed: %q", got)
	}
	src := mdxSample + "\n<Tabs>\n<TabItem value=\"b\">\ntight child text\n</TabItem>\n</Tabs>\n"
	wantLine := strings.Count(src[:strings.Index(src, "tight child")], "\n") + 1
	for _, s := range doc.Sections {
		if i := strings.Index(s.Text, "tight child text"); i >= 0 {
			span, _ := SourceRange(s.SourceMap, i, i+len("tight child text"))
			if span.StartLine != wantLine || span.EndLine != wantLine {
				t.Errorf("JSX child span = %+v, want line %d", span, wantLine)
			}
		}
	}
}

func TestMDXMultilineESMAndCodeFences(t *testing.T) {
	src := "import {\n  A,\n  B,\n} from './x'\nexport const meta = {\n  a: 1,\n}\n\n# H\n\n```jsx\n<Foo />\nimport X from 'y'\n```\n\nimport this module later, not ESM\nbecause this line is prose.\n"
	body := bodyText(parseWith(t, "g.mdx", src))
	for _, leak := range []string{"from './x'", "meta", "  A,"} {
		if strings.Contains(body, leak) {
			t.Errorf("ESM %q indexed:\n%s", leak, body)
		}
	}
	for _, want := range []string{"<Foo />", "import X from 'y'", "because this line is prose"} {
		if !strings.Contains(body, want) {
			t.Errorf("%q missing:\n%s", want, body)
		}
	}
}

// The MDX cleanup keys on the extension; plain Markdown is unchanged.
func TestMDXCleanupDoesNotAffectMarkdown(t *testing.T) {
	src := "import a from 'b'\n\n# H\n\n<div>\nhtml text\n</div>\n"
	md := bodyText(parseWith(t, "g.md", src))
	if !strings.Contains(md, "import a from 'b'") || strings.Contains(md, "html text") {
		t.Errorf(".md output changed:\n%s", md)
	}
}
