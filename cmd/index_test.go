package cmd

import (
	"testing"

	"github.com/itsmostafa/qi/internal/config"
)

func TestFindCollectionByPath(t *testing.T) {
	cols := []config.Collection{
		{Name: "notes", Path: "/home/user/notes"},
		{Name: "code", Path: "/home/user/projects"},
	}

	t.Run("found", func(t *testing.T) {
		got := findCollectionByPath(cols, "/home/user/notes")
		if got == nil {
			t.Fatal("expected non-nil result")
		}
		if got.Name != "notes" {
			t.Errorf("expected name %q, got %q", "notes", got.Name)
		}
	})

	t.Run("not found", func(t *testing.T) {
		got := findCollectionByPath(cols, "/home/user/other")
		if got != nil {
			t.Errorf("expected nil, got %+v", got)
		}
	})

	t.Run("empty slice", func(t *testing.T) {
		got := findCollectionByPath(nil, "/home/user/notes")
		if got != nil {
			t.Errorf("expected nil on empty slice, got %+v", got)
		}
	})
}
