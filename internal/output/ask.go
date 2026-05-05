package output

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strings"

	"github.com/itsmostafa/qi/internal/config"
	"github.com/itsmostafa/qi/internal/search"
)

// AskSource is a compact source reference for generated answers.
type AskSource struct {
	Index        int    `json:"index"`
	Indices      []int  `json:"indices"`
	Title        string `json:"title"`
	Path         string `json:"path"`
	Collection   string `json:"collection"`
	RelativePath string `json:"relative_path"`
}

// AskOutput is the structured JSON shape for qi ask.
type AskOutput struct {
	Answer  string      `json:"answer"`
	Sources []AskSource `json:"sources"`
}

// WriteAskResult writes a generated answer and compact, deduplicated sources.
func WriteAskResult(w io.Writer, answer string, results []search.Result, collections []config.Collection, format string) error {
	sources := CompactAskSources(results, collections)
	switch strings.ToLower(format) {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(AskOutput{Answer: answer, Sources: sources})
	case "markdown", "md":
		fmt.Fprintln(w, answer)
		if len(sources) == 0 {
			return nil
		}
		fmt.Fprintln(w)
		fmt.Fprintln(w, "## Sources")
		return WriteAskSourcesMarkdown(w, sources)
	default:
		fmt.Fprintln(w, answer)
		if len(sources) == 0 {
			return nil
		}
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Sources:")
		return WriteAskSources(w, sources)
	}
}

// CompactAskSources converts chunk-level search hits into document-level source rows.
func CompactAskSources(results []search.Result, collections []config.Collection) []AskSource {
	bases := collectionPathBases(collections)
	sourceByKey := map[string]int{}
	sources := make([]AskSource, 0, len(results))

	for i, r := range results {
		promptIndex := i + 1
		key := r.Collection + "\x00" + r.Path
		if sourceIndex, ok := sourceByKey[key]; ok {
			sources[sourceIndex].Indices = append(sources[sourceIndex].Indices, promptIndex)
			continue
		}
		sourceByKey[key] = len(sources)

		title := strings.TrimSpace(r.Title)
		if title == "" {
			title = r.Path
		}

		source := AskSource{
			Index:        promptIndex,
			Indices:      []int{promptIndex},
			Title:        title,
			Path:         displaySourcePath(r, bases),
			Collection:   r.Collection,
			RelativePath: filepath.ToSlash(r.Path),
		}
		sources = append(sources, source)
	}

	return sources
}

// WriteAskSources writes compact source rows.
func WriteAskSources(w io.Writer, sources []AskSource) error {
	for _, source := range sources {
		fmt.Fprintf(w, "%s %s\n", formatSourceIndices(source), source.Title)
		fmt.Fprintf(w, "    %s\n", source.Path)
	}
	return nil
}

// WriteAskSourcesMarkdown writes compact source rows as Markdown.
func WriteAskSourcesMarkdown(w io.Writer, sources []AskSource) error {
	for _, source := range sources {
		fmt.Fprintf(w, "- %s **%s**\n", formatSourceIndices(source), source.Title)
		fmt.Fprintf(w, "  `%s`\n", source.Path)
	}
	return nil
}

func formatSourceIndices(source AskSource) string {
	indices := source.Indices
	if len(indices) == 0 {
		indices = []int{source.Index}
	}

	parts := make([]string, 0, len(indices))
	for _, index := range indices {
		parts = append(parts, fmt.Sprintf("%d", index))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func collectionPathBases(collections []config.Collection) map[string]string {
	bases := make(map[string]string, len(collections))
	for _, col := range collections {
		base := collectionPathBase(col)
		bases[col.Name] = base
		if col.OriginalName != "" {
			bases[col.OriginalName] = base
		}
	}
	return bases
}

func collectionPathBase(col config.Collection) string {
	if col.Path == "" {
		return col.Name
	}
	base := filepath.Base(filepath.Clean(col.Path))
	if base == "." || base == string(filepath.Separator) || base == "" {
		return col.Name
	}
	return base
}

func displaySourcePath(r search.Result, bases map[string]string) string {
	base := bases[r.Collection]
	if base == "" {
		base = r.Collection
	}
	relPath := filepath.ToSlash(r.Path)
	base = filepath.ToSlash(base)
	if relPath == "" {
		return base
	}
	return path.Join(base, relPath)
}
