//go:build aix || android || darwin || dragonfly || freebsd || hurd || illumos || ios || linux || netbsd || openbsd || solaris

package indexer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

type secureRoot struct {
	fd int
}

// openSecureRoot traverses an absolute directory path from the filesystem root
// without following a component replaced by a symlink after canonicalization.
func openSecureRoot(path string) (*secureRoot, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("collection root is not absolute: %q", path)
	}
	fd, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Clean(path), string(filepath.Separator)), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		next, openErr := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		_ = unix.Close(fd)
		if openErr != nil {
			return nil, openErr
		}
		fd = next
	}
	return &secureRoot{fd: fd}, nil
}

func (r *secureRoot) Close() error {
	return unix.Close(r.fd)
}

// ReadFile opens every component relative to the pinned root. O_NOFOLLOW
// closes final-file and intermediate-directory symlink replacement races.
func (r *secureRoot) ReadFile(relPath string) ([]byte, error) {
	parts := strings.Split(filepath.ToSlash(filepath.Clean(relPath)), "/")
	if len(parts) == 0 || relPath == "." {
		return nil, fmt.Errorf("invalid relative path %q", relPath)
	}
	fd, err := unix.Dup(r.fd)
	if err != nil {
		return nil, err
	}
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			_ = unix.Close(fd)
			return nil, fmt.Errorf("unsafe path component %q", part)
		}
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW
		if i < len(parts)-1 {
			flags |= unix.O_DIRECTORY
		}
		next, openErr := unix.Openat(fd, part, flags, 0)
		_ = unix.Close(fd)
		if openErr != nil {
			return nil, openErr
		}
		fd = next
	}
	file := os.NewFile(uintptr(fd), relPath)
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("creating file handle")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file")
	}
	if info.Size() > maxFileSize {
		return nil, fmt.Errorf("file is %d bytes, over the %d-byte limit", info.Size(), maxFileSize)
	}
	// Bound the read too, not just the Stat: a writer appending after the check
	// would otherwise grow the file past the cap while it is being read.
	data, err := io.ReadAll(io.LimitReader(file, maxFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFileSize {
		return nil, fmt.Errorf("file grew past the %d-byte limit while being read", maxFileSize)
	}
	return data, nil
}
