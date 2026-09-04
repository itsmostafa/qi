package app

import (
	"reflect"
	"testing"

	"github.com/itsmostafa/qi/internal/config"
)

// The collection whose legacy name another collection is vacating must still be
// migrated; skipping it stranded its rows and let the other one land on top.
func TestLegacyRenamesCoverChainedNames(t *testing.T) {
	got := legacyRenames([]config.Collection{
		{Path: "/y/x-foo", Name: "x-foo", OriginalName: "y-x-foo"},
		{Path: "/x/foo", Name: "foo", OriginalName: "x-foo"},
	})
	want := [][2]string{{"y-x-foo", "x-foo"}, {"x-foo", "foo"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("legacyRenames = %v, want %v", got, want)
	}
}

// A legacy name that is some other collection's settled name is ambiguous.
func TestLegacyRenamesSkipsSettledName(t *testing.T) {
	got := legacyRenames([]config.Collection{
		{Path: "/a/notes", Name: "notes", OriginalName: ""},
		{Path: "/b/docs", Name: "docs", OriginalName: "notes"},
	})
	if len(got) != 0 {
		t.Errorf("legacyRenames = %v, want none", got)
	}
}
