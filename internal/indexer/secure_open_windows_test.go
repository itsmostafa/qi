//go:build windows

package indexer

import "testing"

func TestWindowsSecureOpenFailsClosed(t *testing.T) {
	if root, err := openSecureRoot(`C:\collection`); err == nil || root != nil {
		t.Fatalf("Windows secure-open policy must fail closed, root=%v err=%v", root, err)
	}
}
