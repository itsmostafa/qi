package search

import (
	"strings"
	"testing"
)

func TestBuildPromptUsesNumberedCitationContract(t *testing.T) {
	systemPrompt, userPrompt := BuildPrompt("How should citations work?", []Result{
		{
			Collection:  "notes",
			Path:        "guide.md",
			HeadingPath: "Usage > Citations",
			Snippet:     "Use citations next to claims.",
		},
		{
			Collection: "docs",
			Path:       "reference.md",
			Snippet:    "More citation detail.",
		},
	})

	if !strings.Contains(systemPrompt, "numeric citation marker") {
		t.Fatalf("system prompt does not instruct numbered citations:\n%s", systemPrompt)
	}
	if strings.Contains(systemPrompt, "[qi://collection/path]") {
		t.Fatalf("system prompt still instructs URI-shaped citations:\n%s", systemPrompt)
	}

	for _, want := range []string{
		"### [1] qi://notes/guide.md (Usage > Citations)",
		"### [2] qi://docs/reference.md",
		"## Question\n\nHow should citations work?",
	} {
		if !strings.Contains(userPrompt, want) {
			t.Fatalf("user prompt missing %q:\n%s", want, userPrompt)
		}
	}
}
