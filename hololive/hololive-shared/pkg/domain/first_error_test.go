package domain

import (
	"errors"
	"testing"
)

func TestFirstErrorReturnsFirstNonNilAndIgnoresRest(t *testing.T) {
	first := errors.New("first")
	second := errors.New("second")

	got := firstError(nil, first, second)
	if !errors.Is(got, first) {
		t.Fatalf("firstError returned %v, want first error %v", got, first)
	}
	if errors.Is(got, second) {
		t.Fatalf("firstError must not join subsequent errors; got %v also matches second", got)
	}
}

func TestFirstErrorReturnsNilWhenAllNil(t *testing.T) {
	if got := firstError(nil, nil); got != nil {
		t.Fatalf("firstError(nil, nil) = %v, want nil", got)
	}
}
