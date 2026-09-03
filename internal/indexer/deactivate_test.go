package indexer

import (
	"errors"
	"strings"
	"testing"
)

type failingRows struct {
	scanErr error
	rowsErr error
	next    bool
}

func (r *failingRows) Next() bool {
	if r.next {
		r.next = false
		return true
	}
	return false
}
func (r *failingRows) Scan(...any) error { return r.scanErr }
func (r *failingRows) Err() error        { return r.rowsErr }

func TestMissingDocumentIDsReturnsScanError(t *testing.T) {
	_, err := missingDocumentIDs(&failingRows{next: true, scanErr: errors.New("scan failed")}, nil)
	if err == nil || !strings.Contains(err.Error(), "scan failed") {
		t.Fatalf("expected scan error, got %v", err)
	}
}

func TestMissingDocumentIDsReturnsRowsError(t *testing.T) {
	_, err := missingDocumentIDs(&failingRows{rowsErr: errors.New("iteration failed")}, nil)
	if err == nil || !strings.Contains(err.Error(), "iteration failed") {
		t.Fatalf("expected rows error, got %v", err)
	}
}
