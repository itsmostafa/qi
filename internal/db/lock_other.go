//go:build !unix

package db

// LockIndex is a no-op where flock is unavailable; overlapping runs fall back
// to SQLite's busy timeout.
func LockIndex(dbPath string, waiting func()) (unlock func(), err error) {
	return func() {}, nil
}
