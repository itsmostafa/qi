package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/itsmostafa/qi/internal/app"
	"github.com/spf13/cobra"
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show index statistics",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		a, err := app.New(ctx, cfgFile)
		if err != nil {
			return err
		}
		defer a.Close()

		type collectionStats struct {
			name      string
			documents int
			chunks    int
			embeddings int
		}

		var collections []collectionStats
		rows, err := a.DB.QueryContext(ctx, `
			SELECT
				d.collection,
				COUNT(DISTINCT d.id)  AS docs,
				COUNT(DISTINCT c.id)  AS chunks,
				COUNT(DISTINCT e.chunk_id) AS embeddings
			FROM documents d
			LEFT JOIN chunks c ON c.doc_id = d.id
			LEFT JOIN embeddings e ON e.chunk_id = c.id
			WHERE d.active = 1
			GROUP BY d.collection
		`)
		if err != nil {
			return fmt.Errorf("querying stats: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var s collectionStats
			if err := rows.Scan(&s.name, &s.documents, &s.chunks, &s.embeddings); err != nil {
				continue
			}
			collections = append(collections, s)
		}

		// DB file size
		var dbSize int64
		sizeRow := a.DB.QueryRowContext(ctx, `SELECT page_count * page_size FROM pragma_page_count(), pragma_page_size()`)
		_ = sizeRow.Scan(&dbSize)

		// Last index run
		var lastRun string
		runRow := a.DB.QueryRowContext(ctx, `SELECT MAX(finished_at) FROM index_runs WHERE finished_at IS NOT NULL`)
		_ = runRow.Scan(&lastRun)
		if lastRun == "" {
			lastRun = "never"
		}

		// Print
		fmt.Fprintf(os.Stdout, "qi statistics\n\n")
		if len(collections) == 0 {
			fmt.Fprintln(os.Stdout, "No indexed documents. Run `qi index` to get started.")
		}
		for _, s := range collections {
			fmt.Fprintf(os.Stdout, "  Collection: %s\n", s.name)
			fmt.Fprintf(os.Stdout, "    Documents:  %d\n", s.documents)
			fmt.Fprintf(os.Stdout, "    Chunks:     %d\n", s.chunks)
			fmt.Fprintf(os.Stdout, "    Embeddings: %d\n", s.embeddings)
		}
		fmt.Fprintf(os.Stdout, "\n  Database size: %s\n", formatBytes(dbSize))
		fmt.Fprintf(os.Stdout, "  Last indexed:  %s\n", lastRun)
		return nil
	},
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
