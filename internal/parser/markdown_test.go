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
