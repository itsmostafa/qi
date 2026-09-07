package cmd

import (
	"testing"

	"github.com/itsmostafa/qi/internal/search"
)

func TestPassageLimitValidation(t *testing.T) {
	for _, n := range []int{-1, 0, search.MaxPassages, search.MaxPassages + 1} {
		err := validateSearchOpts(search.SearchOpts{Passages: n})
		valid := n >= 0 && n <= search.MaxPassages
		if (err == nil) != valid {
			t.Errorf("passages=%d: got %v, want valid=%v", n, err, valid)
		}
	}
}
