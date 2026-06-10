package deliverysql

import (
	"testing"
	"time"
)

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		maxLen int
		want   string
	}{
		{name: "short text unchanged", in: "abc", maxLen: 5, want: "abc"},
		{name: "exact length unchanged", in: "hello", maxLen: 5, want: "hello"},
		{name: "ascii truncated", in: "hello world", maxLen: 8, want: "hello..."},
		{name: "multibyte boundary", in: "안녕하세요세계", maxLen: 6, want: "안녕하..."},
		{name: "minimum ellipsis", in: "abcdef", maxLen: 3, want: "..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TruncateString(tt.in, tt.maxLen); got != tt.want {
				t.Errorf("TruncateString(%q, %d) = %q, want %q", tt.in, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestCloneUTCTimePtr(t *testing.T) {
	if got := CloneUTCTimePtr(nil); got != nil {
		t.Errorf("CloneUTCTimePtr(nil) = %v, want nil", got)
	}

	zero := time.Time{}
	if got := CloneUTCTimePtr(&zero); got != nil {
		t.Errorf("CloneUTCTimePtr(zero) = %v, want nil", got)
	}

	loc := time.FixedZone("KST", 9*60*60)
	local := time.Date(2026, 6, 10, 12, 0, 0, 0, loc)
	got := CloneUTCTimePtr(&local)
	if got == nil {
		t.Fatal("CloneUTCTimePtr(non-utc) = nil, want non-nil")
	}
	if got.Location() != time.UTC {
		t.Errorf("CloneUTCTimePtr location = %v, want UTC", got.Location())
	}
	if !got.Equal(local) {
		t.Errorf("CloneUTCTimePtr instant = %v, want equal to %v", got, local)
	}
	if got == &local {
		t.Error("CloneUTCTimePtr returned the input pointer, want an independent copy")
	}
	got.Add(time.Hour)
	if !local.Equal(time.Date(2026, 6, 10, 12, 0, 0, 0, loc)) {
		t.Error("mutating clone affected the source")
	}
}
