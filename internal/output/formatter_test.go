package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/itsmostafa/qi/internal/search"
)

func TestCitationOutput(t *testing.T) {
	hash := strings.Repeat("ab", 32)
	marked := search.HighlightOpen + "evidence" + search.HighlightClose
	results := []search.Result{{
		Collection: "notes", Path: "a #1.md", Hash: hash,
		SourceURI: search.SourceURI("notes", "a #1.md"),
		StartLine: 4, EndLine: 6, Snippet: marked,
		Passages: []search.Passage{{ChunkID: 2, StartLine: 10, EndLine: 12, Snippet: marked}},
	}}
	for _, format := range []string{"json", "text", "markdown"} {
		t.Run(format, func(t *testing.T) {
			var buf bytes.Buffer
			if err := New(format).WriteResults(&buf, results); err != nil {
				t.Fatal(err)
			}
			if strings.ContainsAny(buf.String(), search.HighlightOpen+search.HighlightClose) {
				t.Fatalf("highlight markers leaked: %q", buf.String())
			}
			if format == "json" {
				var got []search.Result
				if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if len(got) != 1 || got[0].Hash != hash || got[0].SourceURI != results[0].SourceURI || got[0].StartLine != 4 || got[0].EndLine != 6 || len(got[0].Passages) != 1 || got[0].Passages[0].Snippet != "evidence" {
					t.Fatalf("citation metadata lost: %+v", got)
				}
			} else {
				for _, want := range []string{"qi://notes/a%20%231.md#L4-L6", "qi get " + hash + " --lines 4:6", "10:12"} {
					if !strings.Contains(buf.String(), want) {
						t.Errorf("missing %q in %s", want, buf.String())
					}
				}
			}
		})
	}
	if results[0].Snippet != marked || results[0].Passages[0].Snippet != marked {
		t.Fatal("formatting mutated input evidence")
	}
}
