package cmd

import (
	"fmt"
	"time"

	"github.com/itsmostafa/qi/internal/search"
	"github.com/spf13/cobra"
)

const dateLayout = "2006-01-02"

// addRecencyFlags registers the date filters shared by search and query.
func addRecencyFlags(cmd *cobra.Command, since, until, sort *string) {
	cmd.Flags().StringVar(since, "since", "", "only documents dated on or after YYYY-MM-DD")
	cmd.Flags().StringVar(until, "until", "", "only documents dated on or before YYYY-MM-DD")
	cmd.Flags().StringVar(sort, "sort", "", "result order: relevance (default) or date")
}

func validateSearchOpts(opts search.SearchOpts) error {
	for name, value := range map[string]string{"since": opts.Since, "until": opts.Until} {
		if value == "" {
			continue
		}
		if _, err := time.Parse(dateLayout, value); err != nil {
			return fmt.Errorf("--%s must be YYYY-MM-DD, got %q", name, value)
		}
	}
	switch opts.Sort {
	case "", "relevance", "date":
		return nil
	}
	return fmt.Errorf("unknown --sort %q: use relevance or date", opts.Sort)
}
