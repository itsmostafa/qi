package parser

import (
	"strings"
	"testing"
)

func parseWith(t *testing.T, name, src string) *Document {
	t.Helper()
	doc, err := For(name[strings.LastIndex(name, "."):]).Parse(name, []byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return doc
}

// sectionsByPath joins section text per heading path.
func sectionsByPath(doc *Document) map[string]string {
	out := map[string]string{}
	for _, s := range doc.Sections {
		out[s.HeadingPath] += s.Text
	}
	return out
}

func TestRSTRegistered(t *testing.T) {
	if _, ok := For(".rst").(*rstParser); !ok {
		t.Fatalf("For(.rst) = %T", For(".rst"))
	}
}

func TestRSTHeadingsAndTitle(t *testing.T) {
	src := "=====\nGuide\n=====\n\nIntro.\n\nSetup\n-----\n\nSetup body.\n\nDetails\n~~~~~~~\n\nDetail body.\n\nUsage\n-----\n\nUsage body.\n"
	doc := parseWith(t, "g.rst", src)
	if doc.Title != "Guide" {
		t.Errorf("Title = %q, want Guide", doc.Title)
	}
	got := sectionsByPath(doc)
	want := map[string]string{
		"Guide":                   "Intro.",
		"Guide > Setup":           "Setup body.",
		"Guide > Setup > Details": "Detail body.",
		"Guide > Usage":           "Usage body.",
	}
	for path, text := range want {
		if got[path] != text {
			t.Errorf("section %q = %q, want %q (all: %v)", path, got[path], text, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("sections = %v", got)
	}
	// Headings are consumed, not repeated in body text.
	if strings.Contains(bodyText(doc), "=====") || strings.Contains(bodyText(doc), "-----") {
		t.Errorf("adornment leaked into body:\n%s", bodyText(doc))
	}
}

// Overlined and underline-only titles with the same character are different
// styles, so they get different levels.
func TestRSTOverlineIsItsOwnStyle(t *testing.T) {
	src := "======\n Top\n======\n\nSub\n======\n\nbody\n"
	doc := parseWith(t, "g.rst", src)
	if got := doc.Sections[0].HeadingPath; got != "Top > Sub" {
		t.Errorf("HeadingPath = %q, want %q", got, "Top > Sub")
	}
}

func TestRSTFalsePositives(t *testing.T) {
	for name, src := range map[string]string{
		"transition":    "Intro\n=====\n\nbefore\n\n----------\n\nafter\n",
		"simple table":  "Intro\n=====\n\n=====  =====\ncol a  col b\n=====  =====\nx      y\n=====  =====\n",
		"grid table":    "Intro\n=====\n\n+-------+\n| cell  |\n+=======+\n| x     |\n+-------+\n",
		"literal block": "Intro\n=====\n\nExample::\n\n    Fake\n    ====\n\n  ======\n  Fake\n  ======\n",
		"short":         "Intro\n=====\n\nA long title line\n---\n",
		"mid paragraph": "Intro\n=====\n\nfirst line\nsecond line\n-----------\n",
	} {
		doc := parseWith(t, "g.rst", src)
		for _, s := range doc.Sections {
			if s.HeadingPath != "Intro" {
				t.Errorf("%s: unexpected heading path %q (sections %+v)", name, s.HeadingPath, doc.Sections)
			}
		}
	}
}

func TestRSTSourceLines(t *testing.T) {
	src := "Title\n=====\n\npara one\n\nNext\n----\n\npara two\nline three\n"
	doc := parseWith(t, "g.rst", src)
	if len(doc.Sections) != 2 {
		t.Fatalf("sections = %+v", doc.Sections)
	}
	for i, want := range [][]int{{4}, {9, 10}} {
		spans := sourceLineSpans(t, doc.Sections[i])
		for j, line := range want {
			if spans[j].StartLine != line || spans[j].EndLine != line {
				t.Errorf("section %d line %d span = %+v, want line %d", i, j, spans[j], line)
			}
		}
	}
}

func TestRSTHeadingOnlyDocumentIsSearchable(t *testing.T) {
	doc := parseWith(t, "g.rst", "Unicorn\n=======\n\nDeploy\n------\n")
	if len(doc.Sections) != 1 || doc.Sections[0].Text != "Unicorn\nDeploy" {
		t.Fatalf("sections = %+v", doc.Sections)
	}
}

func TestRSTByteOrderMark(t *testing.T) {
	doc := parseWith(t, "a.rst", "\xef\xbb\xbfTitle\n=====\n\nBody\n")
	if doc.Title != "Title" {
		t.Fatalf("Title = %q", doc.Title)
	}
}
