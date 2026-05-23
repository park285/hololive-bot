package util

import (
	"testing"
	"time"
)

func TestToKST(t *testing.T) {
	t.Parallel()

	utc := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
	got := ToKST(utc)

	if got.Hour() != 9 {
		t.Fatalf("ToKST(2025-06-15T00:00Z).Hour() = %d, want 9", got.Hour())
	}
	if got.Location().String() != "Asia/Seoul" && got.Location().String() != "KST" {
		t.Fatalf("ToKST().Location() = %q, want Asia/Seoul or KST", got.Location().String())
	}
}

func TestFormatKST(t *testing.T) {
	t.Parallel()

	utc := time.Date(2025, 1, 1, 15, 30, 0, 0, time.UTC)
	got := FormatKST(utc, "2006-01-02 15:04")

	if got != "2025-01-02 00:30" {
		t.Fatalf("FormatKST() = %q, want %q", got, "2025-01-02 00:30")
	}
}

func TestNowKST(t *testing.T) {
	t.Parallel()

	before := time.Now()
	got := NowKST()
	after := time.Now()

	if got.Before(before.Add(-time.Second)) || got.After(after.Add(time.Second)) {
		t.Fatalf("NowKST() = %v, outside [%v, %v] window", got, before, after)
	}

	loc := got.Location().String()
	if loc != "Asia/Seoul" && loc != "KST" {
		t.Fatalf("NowKST().Location() = %q, want Asia/Seoul or KST", loc)
	}
}
