package config

import (
	"path"
	"path/filepath"
	"strings"
)

// PathMatch reports whether a collection-relative path is covered by a glob
// pattern. One dialect serves both the `ignore` list and the search `--path`
// filter: path.Match semantics applied to the path, its basename, and every
// ancestor, so `notes` and `notes/*` both cover `notes/a/b.md` and `*.tmp`
// matches at any depth. A pattern without metacharacters still behaves as the
// exact name match `ignore` has always been.
//
// ponytail: no negation, no `.gitignore` parsing. Either needs a dependency or
// a rule engine; a gitignore-compatible matcher is the upgrade path if
// collections ever point at working trees rather than notes.
func PathMatch(pattern, rel string) bool {
	pattern = strings.TrimSuffix(pattern, "/")
	rel = path.Clean(filepath.ToSlash(rel))
	for p := rel; p != "." && p != "/"; p = path.Dir(p) {
		if ok, _ := path.Match(pattern, p); ok {
			return true
		}
		if ok, _ := path.Match(pattern, path.Base(p)); ok {
			return true
		}
	}
	return false
}

// ValidPattern rejects a malformed glob, which would otherwise silently match
// nothing.
func ValidPattern(pattern string) error {
	_, err := path.Match(pattern, "")
	return err
}
