package output

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/itsmostafa/qi/internal/config"
	"github.com/itsmostafa/qi/internal/search"
)

func TestCompactAskSourcesDeduplicatesAndUsesTreeRelativePaths(t *testing.T) {
	collections := []config.Collection{{
		Name: "Projects-tools-qi",
		Path: filepath.Join(t.TempDir(), "qi"),
	}}
	results := []search.Result{
		{
			Collection:  "Projects-tools-qi",
			Path:        "README.md",
			Title:       "qi - query engine cli for ai agents and humans",
			HeadingPath: "Intro",
			Snippet:     "Save tokens by delegating work to <b>qi</b>.",
			Score:       0.0328,
		},
		{
			Collection:  "Projects-tools-qi",
			Path:        "README.md",
			Title:       "qi - query engine cli for ai agents and humans",
			HeadingPath: "Install",
			Snippet:     "/plugin install <b>qi</b>",
			Score:       0.0310,
		},
		{
			Collection: "Projects-tools-qi",
			Path:       "skills/qi-cli/SKILL.md",
			Title:      "qi-cli",
			Snippet:    "Prefer <b>qi</b> query.",
			Score:      0.0263,
		},
	}

	sources := CompactAskSources(results, collections)

	if got, want := len(sources), 2; got != want {
		t.Fatalf("source count = %d, want %d", got, want)
	}
	if got, want := sources[0].Index, 1; got != want {
		t.Fatalf("first index = %d, want %d", got, want)
	}
	if got, want := sources[0].Indices, []int{1, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("first indices = %v, want %v", got, want)
	}
	if got, want := sources[0].Path, "qi/README.md"; got != want {
		t.Fatalf("first path = %q, want %q", got, want)
	}
	if got, want := sources[1].Index, 3; got != want {
		t.Fatalf("second index = %d, want %d", got, want)
	}
	if got, want := sources[1].Indices, []int{3}; !reflect.DeepEqual(got, want) {
		t.Fatalf("second indices = %v, want %v", got, want)
	}
	if got, want := sources[1].Path, "qi/skills/qi-cli/SKILL.md"; got != want {
		t.Fatalf("second path = %q, want %q", got, want)
	}
}

func TestWriteAskResultTextOmitsSearchDetails(t *testing.T) {
	collections := []config.Collection{{
		Name: "Projects-tools-qi",
		Path: filepath.Join(t.TempDir(), "qi"),
	}}
	results := []search.Result{
		{
			Collection:  "Projects-tools-qi",
			Path:        "README.md",
			Title:       "qi - query engine cli for ai agents and humans",
			HeadingPath: "Search Modes",
			Snippet:     "Use <b>qi</b> query --mode hybrid.",
			Score:       0.0328,
		},
		{
			Collection:  "Projects-tools-qi",
			Path:        "README.md",
			Title:       "qi - query engine cli for ai agents and humans",
			HeadingPath: "Install",
			Snippet:     "Install <b>qi</b> locally.",
			Score:       0.0310,
		},
		{
			Collection: "Projects-tools-qi",
			Path:       "skills/qi-cli/SKILL.md",
			Title:      "qi-cli",
			Snippet:    "Prefer <b>qi</b> search.",
			Score:      0.0263,
		},
	}

	var buf bytes.Buffer
	if err := WriteAskResult(&buf, "Using qi saves tokens [2].", results, collections, "text"); err != nil {
		t.Fatalf("writing ask result: %v", err)
	}

	want := `Using qi saves tokens [2].

Sources:
[1, 2] qi - query engine cli for ai agents and humans
    qi/README.md
[3] qi-cli
    qi/skills/qi-cli/SKILL.md
`
	if got := buf.String(); got != want {
		t.Fatalf("unexpected text output:\ngot:\n%s\nwant:\n%s", got, want)
	}

	for _, forbidden := range []string{"score:", "0.0328", "<b>", "</b>", "qi://", "Search Modes", "Use qi", "Install qi"} {
		if strings.Contains(buf.String(), forbidden) {
			t.Fatalf("output contains search detail %q:\n%s", forbidden, buf.String())
		}
	}
}

func TestWriteAskResultMarkdownFormatsSources(t *testing.T) {
	collections := []config.Collection{{
		Name: "Projects-tools-qi",
		Path: filepath.Join(t.TempDir(), "qi"),
	}}
	results := []search.Result{
		{
			Collection:  "Projects-tools-qi",
			Path:        "README.md",
			Title:       "qi - query engine cli for ai agents and humans",
			HeadingPath: "Search Modes",
			Snippet:     "Use <b>qi</b> query --mode hybrid.",
			Score:       0.0328,
		},
		{
			Collection:  "Projects-tools-qi",
			Path:        "README.md",
			Title:       "qi - query engine cli for ai agents and humans",
			HeadingPath: "Install",
			Snippet:     "Install <b>qi</b> locally.",
			Score:       0.0310,
		},
		{
			Collection: "Projects-tools-qi",
			Path:       "skills/qi-cli/SKILL.md",
			Title:      "qi-cli",
			Snippet:    "Prefer <b>qi</b> search.",
			Score:      0.0263,
		},
	}

	want := `Using **qi** saves tokens [2].

## Sources
- [1, 2] **qi - query engine cli for ai agents and humans**
  ` + "`qi/README.md`" + `
- [3] **qi-cli**
  ` + "`qi/skills/qi-cli/SKILL.md`" + `
`

	for _, format := range []string{"markdown", "md"} {
		t.Run(format, func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteAskResult(&buf, "Using **qi** saves tokens [2].", results, collections, format); err != nil {
				t.Fatalf("writing markdown ask result: %v", err)
			}
			if got := buf.String(); got != want {
				t.Fatalf("unexpected markdown output:\ngot:\n%s\nwant:\n%s", got, want)
			}
			if strings.Contains(buf.String(), "Sources:") {
				t.Fatalf("markdown output contains plain text sources header:\n%s", buf.String())
			}
		})
	}
}

func TestCompactAskSourcesFallsBackToCollectionName(t *testing.T) {
	sources := CompactAskSources([]search.Result{{
		Collection: "notes",
		Path:       "daily.md",
		Title:      "Daily Notes",
	}}, nil)

	if got, want := sources[0].Path, "notes/daily.md"; got != want {
		t.Fatalf("fallback path = %q, want %q", got, want)
	}
}

func TestWriteAskResultJSON(t *testing.T) {
	collections := []config.Collection{{
		Name: "Projects-tools-qi",
		Path: filepath.Join(t.TempDir(), "qi"),
	}}
	results := []search.Result{
		{
			Collection: "Projects-tools-qi",
			Path:       "README.md",
			Title:      "qi - query engine cli for ai agents and humans",
		},
		{
			Collection: "Projects-tools-qi",
			Path:       "README.md",
			Title:      "qi - query engine cli for ai agents and humans",
		},
	}

	var buf bytes.Buffer
	if err := WriteAskResult(&buf, "Answer [2].", results, collections, "json"); err != nil {
		t.Fatalf("writing json ask result: %v", err)
	}

	var got AskOutput
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshalling json output: %v", err)
	}
	if got.Answer != "Answer [2]." {
		t.Fatalf("answer = %q, want %q", got.Answer, "Answer [2].")
	}
	if len(got.Sources) != 1 {
		t.Fatalf("source count = %d, want 1", len(got.Sources))
	}
	if got.Sources[0].Path != "qi/README.md" {
		t.Fatalf("json source path = %q, want %q", got.Sources[0].Path, "qi/README.md")
	}
	if got.Sources[0].RelativePath != "README.md" {
		t.Fatalf("json relative path = %q, want %q", got.Sources[0].RelativePath, "README.md")
	}
	if got, want := got.Sources[0].Indices, []int{1, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("json indices = %v, want %v", got, want)
	}
}
