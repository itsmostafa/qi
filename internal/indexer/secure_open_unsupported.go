//go:build !windows && !aix && !android && !darwin && !dragonfly && !freebsd && !hurd && !illumos && !ios && !linux && !netbsd && !openbsd && !solaris

package indexer

import "fmt"

// Unsupported platforms fail closed until they gain a descriptor-relative,
// no-follow implementation.
type secureRoot struct{}

func openSecureRoot(path string) (*secureRoot, error) {
	return nil, fmt.Errorf("secure no-follow indexing is not supported on this platform for collection %q", path)
}

func (*secureRoot) Close() error { return nil }

func (*secureRoot) ReadFile(string) ([]byte, error) {
	return nil, fmt.Errorf("secure no-follow indexing is not supported on this platform")
}
