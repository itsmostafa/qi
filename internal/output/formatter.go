package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/itsmostafa/qi/internal/search"
)

// FTS5 wraps matched terms in sentinel bytes. Render them as ANSI bold on a
// terminal and drop them everywhere else — they are markers, not content, and
// as <b> tags they used to leak into JSON and into piped output.
const (
	ansiBold  = "\x1b[1m"
	ansiReset = "\x1b[0m"
)

var highlightStripper = strings.NewReplacer(
	search.HighlightOpen, "",
	search.HighlightClose, "",
)

var highlightANSI = strings.NewReplacer(
	search.HighlightOpen, ansiBold,
	search.HighlightClose, ansiReset,
)

// isTerminal reports whether w is a character device, so escape codes are only
// emitted where something will interpret them.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// stripHighlights returns a copy of results with highlight markers removed, for
// formats that carry no styling.
func stripHighlights(results []search.Result) []search.Result {
	out := make([]search.Result, len(results))
	for i, r := range results {
		r.Snippet = highlightStripper.Replace(r.Snippet)
		out[i] = r
	}
	return out
}

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
	highlight := highlightStripper
	if isTerminal(w) {
		highlight = highlightANSI
	}
	for i, r := range results {
		location := fmt.Sprintf("qi://%s/%s", r.Collection, r.Path)
		if r.HeadingPath != "" {
			location += " [" + r.HeadingPath + "]"
		}
		scale := r.Scale
		if scale == "" {
			scale = "score"
		}
		fmt.Fprintf(w, "%d. %s (%s: %.4f)", i+1, r.Title, scale, r.Score)
		if r.Timestamp != "" {
			fmt.Fprintf(w, " [%s]", r.Timestamp)
		}
		fmt.Fprintln(w)
		fmt.Fprintf(w, "   %s\n", location)
		if r.Snippet != "" {
			fmt.Fprintf(w, "   %s\n", highlight.Replace(r.Snippet))
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
	// Always an array: a nil slice would encode as null and break callers that
	// iterate the response unconditionally.
	if results == nil {
		results = []search.Result{}
	}
	return enc.Encode(stripHighlights(results))
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
			fmt.Fprintf(w, "> %s\n", highlightStripper.Replace(r.Snippet))
		}
		fmt.Fprintln(w)
	}
	return nil
}
