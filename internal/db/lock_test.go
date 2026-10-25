//go:build unix

package db

import (
	"path/filepath"
	"testing"
	"time"
)

// A second run waits for the first rather than failing or being skipped.
func TestLockIndexWaitsForHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "qi.db")
	unlock, err := LockIndex(path, func() { t.Error("first holder should not wait") })
	if err != nil {
		t.Fatal(err)
	}

	acquired := make(chan struct{})
	waited := make(chan struct{}, 1)
	go func() {
		unlock2, err := LockIndex(path, func() { waited <- struct{}{} })
		if err != nil {
			t.Error(err)
			return
		}
		close(acquired)
		unlock2()
	}()

	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("second run did not report waiting")
	}
	select {
	case <-acquired:
		t.Fatal("second run acquired the lock while the first held it")
	case <-time.After(100 * time.Millisecond):
	}
	unlock()
	select {
	case <-acquired:
	case <-time.After(5 * time.Second):
		t.Fatal("second run never acquired the lock after release")
	}
}
