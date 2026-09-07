package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/itsmostafa/qi/internal/app"
	"github.com/itsmostafa/qi/internal/search"
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
	SourceURI  string `json:"source_uri"`
	Title      string `json:"title"`
	Hash       string `json:"hash"`
	Body       string `json:"body"`
	Truncated  bool   `json:"truncated"`
	// AlsoAt names the other documents sharing this content, empty for the
	// usual one-document case.
	AlsoAt []string `json:"also_at,omitempty"`
}

func validateHashPrefix(id string) (string, error) {
	if id == "" || len(id) > 64 {
		return "", fmt.Errorf("ID prefix must contain 1 to 64 hexadecimal characters, got %q", id)
	}
	for _, r := range id {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return "", fmt.Errorf("ID prefix must contain only hexadecimal characters, got %q", id)
		}
	}
	return strings.ToLower(id), nil
}

var getCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Retrieve a document by ID (hash prefix)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := validateHashPrefix(args[0])
		if err != nil {
			return err
		}
		if getMaxBytes < 0 {
			return fmt.Errorf("--max-bytes must not be negative, got %d", getMaxBytes)
		}
		ctx := context.Background()
		a, err := app.New(ctx, cfgFile)
		if err != nil {
			return err
		}
		defer a.Close()

		// First identify distinct hashes. Looking at only the first ten paths can
		// hide a second hash when many documents share one content blob.
		hashRows, err := a.DB.QueryContext(ctx, `
			SELECT DISTINCT d.content_hash
			FROM documents d
			WHERE d.content_hash LIKE ? AND d.active = 1
			ORDER BY d.content_hash
			LIMIT 2
		`, id+"%")
		if err != nil {
			return fmt.Errorf("querying document: %w", err)
		}
		var hashes []string
		for hashRows.Next() {
			var hash string
			if err := hashRows.Scan(&hash); err != nil {
				hashRows.Close()
				return fmt.Errorf("reading document hash: %w", err)
			}
			hashes = append(hashes, hash)
		}
		if err := hashRows.Err(); err != nil {
			hashRows.Close()
			return fmt.Errorf("reading document hashes: %w", err)
		}
		hashRows.Close()
		switch len(hashes) {
		case 0:
			return fmt.Errorf("no document found with ID prefix %q", id)
		case 2:
			var b strings.Builder
			fmt.Fprintf(&b, "ID prefix %q is ambiguous; distinct hashes:", id)
			for _, hash := range hashes {
				fmt.Fprintf(&b, "\n  %s", hash)
			}
			b.WriteString("\n\nRetry with a longer prefix.")
			return fmt.Errorf("%s", b.String())
		}

		// LIMIT bounds a potentially very large duplicate-content path list.
		// ponytail: only the first ten locations are reported; page this list if
		// a caller ever needs every duplicate path.
		rows, err := a.DB.QueryContext(ctx, `
			SELECT d.collection, d.path, COALESCE(d.title, ''), d.content_hash, c.body
			FROM documents d
			JOIN content c ON c.hash = d.content_hash
			WHERE d.content_hash = ? AND d.active = 1
			ORDER BY d.collection, d.path
			LIMIT 10
		`, hashes[0])
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
			c.SourceURI = search.SourceURI(c.Collection, c.Path)
			matches = append(matches, c)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("reading documents: %w", err)
		}

		if len(matches) == 0 {
			return fmt.Errorf("no document found with ID prefix %q", id)
		}

		// Identical files dedupe to one content row, so several documents can
		// share a hash. That is not an ambiguous prefix — no longer prefix
		// exists — so return the content and list the locations found.
		doc := matches[0]
		for _, m := range matches[1:] {
			doc.AlsoAt = append(doc.AlsoAt, search.SourceURI(m.Collection, m.Path))
		}
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
		fmt.Fprintf(os.Stdout, "Path:       %s\n", search.SourceURI(doc.Collection, doc.Path))
		for _, p := range doc.AlsoAt {
			fmt.Fprintf(os.Stdout, "Also at:    %s\n", p)
		}
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
