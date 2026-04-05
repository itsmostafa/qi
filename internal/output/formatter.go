package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/itsmostafa/qi/internal/search"
)

// Formatter writes search results to a writer.
type Formatter interface {
	WriteResults(w io.Writer, results []search.Result) error
}

// New returns the formatter for the given format string.
func New(format string) Formatter {
	switch strings.ToLower(format) {
	case "json":
		return &JSONFormatter{}
	case "markdown", "md":
		return &MarkdownFormatter{}
	default:
		return &TextFormatter{}
	}
}

// TextFormatter writes human-readable results.
type TextFormatter struct{}

func (f *TextFormatter) WriteResults(w io.Writer, results []search.Result) error {
	if len(results) == 0 {
		fmt.Fprintln(w, "No results found.")
		return nil
	}
	for i, r := range results {
		location := fmt.Sprintf("qi://%s/%s", r.Collection, r.Path)
		if r.HeadingPath != "" {
			location += " [" + r.HeadingPath + "]"
		}
		fmt.Fprintf(w, "%d. %s (score: %.4f)\n", i+1, r.Title, r.Score)
		fmt.Fprintf(w, "   %s\n", location)
		if r.Snippet != "" {
			fmt.Fprintf(w, "   %s\n", r.Snippet)
		}
		if r.Explain != nil {
			ex := r.Explain
			fmt.Fprintf(w, "   [explain] bm25=%.4f rank=%d", ex.BM25Score, ex.BM25Rank)
			if ex.VectorRank > 0 {
				fmt.Fprintf(w, " vec_dist=%.4f vec_rank=%d", ex.VectorDist, ex.VectorRank)
			}
			if ex.RRFScore > 0 {
				fmt.Fprintf(w, " rrf=%.4f", ex.RRFScore)
			}
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w)
	}
	return nil
}

// JSONFormatter writes results as a JSON array.
type JSONFormatter struct{}

func (f *JSONFormatter) WriteResults(w io.Writer, results []search.Result) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(results)
}

// MarkdownFormatter writes results as a Markdown list.
type MarkdownFormatter struct{}

func (f *MarkdownFormatter) WriteResults(w io.Writer, results []search.Result) error {
	if len(results) == 0 {
		fmt.Fprintln(w, "_No results found._")
		return nil
	}
	for _, r := range results {
		location := fmt.Sprintf("qi://%s/%s", r.Collection, r.Path)
		fmt.Fprintf(w, "### %s\n", r.Title)
		fmt.Fprintf(w, "**Location:** `%s`", location)
		if r.HeadingPath != "" {
			fmt.Fprintf(w, " › %s", r.HeadingPath)
		}
		fmt.Fprintln(w)
		if r.Snippet != "" {
			fmt.Fprintf(w, "> %s\n", r.Snippet)
		}
		fmt.Fprintln(w)
	}
	return nil
}
