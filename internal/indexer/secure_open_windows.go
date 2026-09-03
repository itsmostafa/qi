//go:build windows

package indexer

import "fmt"

// Windows needs handle-relative opens with reparse-point checks to provide the
// same guarantee as openat(O_NOFOLLOW). Until that implementation exists,
// indexing is disabled rather than falling back to path checks followed by
// os.ReadFile, which would reintroduce symlink/junction traversal races.
type secureRoot struct{}

func openSecureRoot(path string) (*secureRoot, error) {
	return nil, fmt.Errorf("secure no-follow indexing is not supported on Windows for collection %q", path)
}

func (*secureRoot) Close() error { return nil }

func (*secureRoot) ReadFile(string) ([]byte, error) {
	return nil, fmt.Errorf("secure no-follow indexing is not supported on Windows")
}
