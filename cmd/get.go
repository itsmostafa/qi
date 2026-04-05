package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/itsmostafa/qi/internal/app"
	"github.com/spf13/cobra"
)

var getCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Retrieve a document or chunk by ID (6-char hash prefix)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		ctx := context.Background()
		a, err := app.New(ctx, cfgFile)
		if err != nil {
			return err
		}
		defer a.Close()

		// Try to match by content hash prefix (documents)
		rows, err := a.DB.QueryContext(ctx, `
			SELECT d.collection, d.path, d.title, d.content_hash, c.body
			FROM documents d
			JOIN content c ON c.hash = d.content_hash
			WHERE d.content_hash LIKE ? AND d.active = 1
			LIMIT 5
		`, id+"%")
		if err != nil {
			return fmt.Errorf("querying document: %w", err)
		}
		defer rows.Close()

		found := false
		for rows.Next() {
			found = true
			var collection, path, title, hash string
			var body []byte
			if err := rows.Scan(&collection, &path, &title, &hash, &body); err != nil {
				continue
			}
			fmt.Fprintf(os.Stdout, "# %s\n", title)
			fmt.Fprintf(os.Stdout, "ID:         #%s\n", hash[:6])
			fmt.Fprintf(os.Stdout, "Collection: %s\n", collection)
			fmt.Fprintf(os.Stdout, "Path:       qi://%s/%s\n", collection, path)
			fmt.Fprintf(os.Stdout, "Hash:       %s\n\n", hash)
			fmt.Fprintf(os.Stdout, "%s\n", body)
		}

		if !found {
			return fmt.Errorf("no document found with ID prefix %q", id)
		}
		return nil
	},
}
