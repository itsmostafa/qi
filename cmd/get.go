package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/itsmostafa/qi/internal/app"
	"github.com/spf13/cobra"
)

var (
	getLines    string
	getMaxBytes int
)

// candidate is one document matching the requested hash prefix.
type candidate struct {
	Collection string `json:"collection"`
	Path       string `json:"path"`
	Title      string `json:"title"`
	Hash       string `json:"hash"`
	Body       string `json:"body"`
	Truncated  bool   `json:"truncated"`
}

var getCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Retrieve a document by ID (hash prefix)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		ctx := context.Background()
		a, err := app.New(ctx, cfgFile)
		if err != nil {
			return err
		}
		defer a.Close()

		// Match by content hash prefix. LIMIT bounds an over-short prefix.
		rows, err := a.DB.QueryContext(ctx, `
			SELECT d.collection, d.path, COALESCE(d.title, ''), d.content_hash, c.body
			FROM documents d
			JOIN content c ON c.hash = d.content_hash
			WHERE d.content_hash LIKE ? AND d.active = 1
			LIMIT 10
		`, id+"%")
		if err != nil {
			return fmt.Errorf("querying document: %w", err)
		}
		defer rows.Close()

		var matches []candidate
		for rows.Next() {
			var c candidate
			if err := rows.Scan(&c.Collection, &c.Path, &c.Title, &c.Hash, &c.Body); err != nil {
				return fmt.Errorf("reading document: %w", err)
			}
			matches = append(matches, c)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("reading documents: %w", err)
		}

		switch {
		case len(matches) == 0:
			return fmt.Errorf("no document found with ID prefix %q", id)
		case len(matches) > 1:
			// Printing every match silently was a correctness bug: the caller
			// could not tell one document from a pile of them.
			var b strings.Builder
			fmt.Fprintf(&b, "ID prefix %q is ambiguous; matches:", id)
			for _, c := range matches {
				fmt.Fprintf(&b, "\n  %s  qi://%s/%s", c.Hash, c.Collection, c.Path)
			}
			b.WriteString("\n\nRetry with a longer prefix.")
			return fmt.Errorf("%s", b.String())
		}

		if getMaxBytes < 0 {
			return fmt.Errorf("--max-bytes must not be negative, got %d", getMaxBytes)
		}
		doc := matches[0]
		doc.Body, err = sliceLines(doc.Body, getLines)
		if err != nil {
			return err
		}
		full := len(doc.Body)
		if getMaxBytes > 0 && full > getMaxBytes {
			// Cut on a byte boundary, then drop a partial trailing rune.
			doc.Body = strings.ToValidUTF8(doc.Body[:getMaxBytes], "")
			doc.Truncated = true
		}

		if strings.ToLower(format) == "json" {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(doc)
		}

		fmt.Fprintf(os.Stdout, "# %s\n", doc.Title)
		fmt.Fprintf(os.Stdout, "ID:         #%s\n", doc.Hash[:6])
		fmt.Fprintf(os.Stdout, "Collection: %s\n", doc.Collection)
		fmt.Fprintf(os.Stdout, "Path:       qi://%s/%s\n", doc.Collection, doc.Path)
		fmt.Fprintf(os.Stdout, "Hash:       %s\n\n", doc.Hash)
		fmt.Fprintf(os.Stdout, "%s\n", doc.Body)
		if doc.Truncated {
			fmt.Fprintf(os.Stdout, "[truncated: %d of %d bytes]\n", len(doc.Body), full)
		}
		return nil
	},
}

// sliceLines returns the 1-indexed inclusive line range spec ("A:B", "A:", ":B")
// of body. An empty spec returns body unchanged.
func sliceLines(body, spec string) (string, error) {
	if spec == "" {
		return body, nil
	}
	from, to, ok := strings.Cut(spec, ":")
	if !ok {
		return "", fmt.Errorf("--lines wants A:B (1-indexed, inclusive), got %q", spec)
	}
	lines := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
	start, end := 1, len(lines)
	var err error
	if from != "" {
		if start, err = strconv.Atoi(from); err != nil || start < 1 {
			return "", fmt.Errorf("--lines start must be a positive integer, got %q", from)
		}
	}
	if to != "" {
		if end, err = strconv.Atoi(to); err != nil || end < 1 {
			return "", fmt.Errorf("--lines end must be a positive integer, got %q", to)
		}
	}
	if start > end {
		return "", fmt.Errorf("--lines start %d is after end %d", start, end)
	}
	if start > len(lines) {
		return "", fmt.Errorf("--lines start %d is past the last line (%d)", start, len(lines))
	}
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[start-1:end], "\n"), nil
}

func init() {
	getCmd.Flags().StringVar(&getLines, "lines", "", "line range A:B, 1-indexed and inclusive; either side may be omitted (10:, :10)")
	getCmd.Flags().IntVar(&getMaxBytes, "max-bytes", 0, "truncate the document body to at most N bytes (0 = no limit)")
}
