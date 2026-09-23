//go:build unix

package db

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// LockIndex serializes index runs against one database. Two pulls close
// together start two background `qi index` runs, and a statement that holds
// the write lock past busyTimeout (a VACUUM or a full FTS optimize at scale)
// failed the second run with "database is locked" — dropping its changes until
// the next pull. The second run waits here instead, however long the first
// takes. waiting is called once if the lock is already held.
//
// An flock is released by the kernel when the process exits, so a crashed or
// killed run never leaves a stale lock behind.
func LockIndex(dbPath string, waiting func()) (unlock func(), err error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, fmt.Errorf("creating db dir: %w", err)
	}
	f, err := os.OpenFile(dbPath+".index-lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening index lock: %w", err)
	}
	fd := int(f.Fd())
	err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) {
		if waiting != nil {
			waiting()
		}
		for err = unix.Flock(fd, unix.LOCK_EX); errors.Is(err, unix.EINTR); {
			err = unix.Flock(fd, unix.LOCK_EX)
		}
	}
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("locking index: %w", err)
	}
	return func() { _ = f.Close() }, nil
}
