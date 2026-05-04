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
	seen := map[string]bool{}
	sources := make([]AskSource, 0, len(results))

	for _, r := range results {
		key := r.Collection + "\x00" + r.Path
		if seen[key] {
			continue
		}
		seen[key] = true

		title := strings.TrimSpace(r.Title)
		if title == "" {
			title = r.Path
		}

		source := AskSource{
			Index:        len(sources) + 1,
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
		fmt.Fprintf(w, "[%d] %s\n", source.Index, source.Title)
		fmt.Fprintf(w, "    %s\n", source.Path)
	}
	return nil
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
