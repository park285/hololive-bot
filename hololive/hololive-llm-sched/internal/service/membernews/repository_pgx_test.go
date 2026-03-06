package membernews

import (
	"errors"
	"strings"
	"testing"
)

func TestNewPGXMemberNewsQuerier_NilPool(t *testing.T) {
	if got := newPGXMemberNewsQuerier(nil); got != nil {
		t.Fatalf("expected nil querier for nil pool")
	}
}

func TestNilRowScanner_Scan(t *testing.T) {
	if err := (nilRowScanner{}).Scan(); err == nil || !strings.Contains(err.Error(), "row scanner is nil") {
		t.Fatalf("expected default nilRowScanner error, got %v", err)
	}

	injectedErr := errors.New("boom")
	if err := (nilRowScanner{err: injectedErr}).Scan(); !errors.Is(err, injectedErr) {
		t.Fatalf("expected injected error, got %v", err)
	}
}
