package membernews

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestNewPGXMemberNewsQuerier_NilPool(t *testing.T) {
	if got := newPGXMemberNewsQuerier(nil); got != nil {
		t.Fatalf("expected nil querier for nil pool")
	}
}

func TestPGXMemberNewsQuerier_NilPoolErrors(t *testing.T) {
	ctx := context.Background()
	q := &pgxMemberNewsQuerier{}

	if err := q.Exec(ctx, "SELECT 1"); err == nil || !strings.Contains(err.Error(), "membernews pgx pool is nil") {
		t.Fatalf("Exec nil-pool error mismatch: %v", err)
	}

	rows, err := q.Query(ctx, "SELECT 1")
	if err == nil || !strings.Contains(err.Error(), "membernews pgx pool is nil") {
		t.Fatalf("Query nil-pool error mismatch: %v", err)
	}
	if rows != nil {
		t.Fatalf("expected nil rows when pool is nil")
	}

	row := q.QueryRow(ctx, "SELECT 1")
	var exists bool
	if err := row.Scan(&exists); err == nil || !strings.Contains(err.Error(), "membernews pgx pool is nil") {
		t.Fatalf("QueryRow.Scan nil-pool error mismatch: %v", err)
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
