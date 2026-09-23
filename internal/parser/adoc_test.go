package parser

import (
	"strings"
	"testing"
)

func TestAdocRegistered(t *testing.T) {
	for _, ext := range []string{".adoc", ".asciidoc"} {
		if _, ok := For(ext).(*adocParser); !ok {
			t.Errorf("For(%s) = %T", ext, For(ext))
		}
	}
}

func TestAdocHeader(t *testing.T) {
	src := "= Admin Guide\nJane Doe <jane@example.com>\nv1.2, 2026-03-04\n:revdate: 2026-03-04\n:keywords: admin, ops\n:description: How to run it.\n:toc:\n\nPreamble.\n"
	doc := parseWith(t, "g.adoc", src)
	if doc.Title != "Admin Guide" {
		t.Errorf("Title = %q", doc.Title)
	}
	if doc.Meta.Timestamp != "2026-03-04" {
		t.Errorf("Timestamp = %q", doc.Meta.Timestamp)
	}
	if len(doc.Meta.Tags) != 2 || doc.Meta.Tags[0] != "admin" || doc.Meta.Tags[1] != "ops" {
		t.Errorf("Tags = %v", doc.Meta.Tags)
	}
	body := bodyText(doc)
	for _, leak := range []string{":toc:", ":revdate:", "Jane Doe", "v1.2"} {
		if strings.Contains(body, leak) {
			t.Errorf("header line %q leaked into body:\n%s", leak, body)
		}
	}
	// Description and tags stay searchable, cited at their attribute lines.
	summary := doc.Sections[0]
	if summary.Text != "How to run it.\nadmin, ops" {
		t.Fatalf("summary = %q", summary.Text)
	}
	spans := sourceLineSpans(t, summary)
	if spans[0].StartLine != 6 || spans[1].StartLine != 5 {
		t.Errorf("summary spans = %+v, want lines 6 and 5", spans)
	}
	if last := doc.Sections[len(doc.Sections)-1]; last.HeadingPath != "Admin Guide" || last.Text != "Preamble." {
		t.Errorf("preamble section = %+v", last)
	}
}

func TestAdocDateAttributeAndTags(t *testing.T) {
	doc := parseWith(t, "g.adoc", "= T\n:date: 2026-01-02\n:tags: a,b\n\nbody\n")
	if doc.Meta.Timestamp != "2026-01-02" || len(doc.Meta.Tags) != 2 {
		t.Errorf("Meta = %+v", doc.Meta)
	}
}

func TestAdocHeadingPaths(t *testing.T) {
	src := "= Guide\n\nIntro.\n\n== Install\n\nInstall body.\n\n=== Verify ===\n\nVerify body.\n\n## Upgrade\n\nUpgrade body.\n"
	got := sectionsByPath(parseWith(t, "g.adoc", src))
	want := map[string]string{
		"Guide":                    "Intro.",
		"Guide > Install":          "Install body.",
		"Guide > Install > Verify": "Verify body.",
		"Guide > Upgrade":          "Upgrade body.",
	}
	for path, text := range want {
		if got[path] != text {
			t.Errorf("section %q = %q, want %q (all: %v)", path, got[path], text, got)
		}
	}
}

func TestAdocSkipsHeadingsInBlocks(t *testing.T) {
	src := "= G\n\n== A\n\n----\n== listing\n----\n\n....\n== literal\n....\n\n====\n== example\n====\n\n```\n== fenced\n```\n\n////\n== commented\nsecret\n////\n\n// == line comment\n\nend\n"
	doc := parseWith(t, "g.adoc", src)
	for _, s := range doc.Sections {
		if s.HeadingPath != "G > A" {
			t.Errorf("unexpected heading path %q", s.HeadingPath)
		}
	}
	body := bodyText(doc)
	for _, want := range []string{"== listing", "== literal", "== example", "== fenced", "end"} {
		if !strings.Contains(body, want) {
			t.Errorf("block content %q missing:\n%s", want, body)
		}
	}
	for _, leak := range []string{"secret", "commented", "line comment"} {
		if strings.Contains(body, leak) {
			t.Errorf("comment %q indexed:\n%s", leak, body)
		}
	}
}

func TestAdocWithoutHeader(t *testing.T) {
	doc := parseWith(t, "g.adoc", "Just text.\n\n== Part\n\nMore.\n")
	if doc.Title != "" {
		t.Errorf("Title = %q, want none", doc.Title)
	}
	got := sectionsByPath(doc)
	if got[""] != "Just text." || got["Part"] != "More." {
		t.Errorf("sections = %v", got)
	}
}
