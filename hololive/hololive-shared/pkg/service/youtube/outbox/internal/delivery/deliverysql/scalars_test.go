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

func TestTruncateStringGuardMatrix(t *testing.T) {
	const (
		short     = "ab"
		long      = "abcdef"
		multibyte = "안녕하세요"
	)
	tests := []struct {
		name   string
		in     string
		maxLen int
		want   string
	}{
		{name: "short/-1", in: short, maxLen: -1, want: ""},
		{name: "short/0", in: short, maxLen: 0, want: ""},
		{name: "short/1", in: short, maxLen: 1, want: "a"},
		{name: "short/2", in: short, maxLen: 2, want: "ab"},
		{name: "short/3", in: short, maxLen: 3, want: "ab"},
		{name: "short/4", in: short, maxLen: 4, want: "ab"},

		{name: "long/-1", in: long, maxLen: -1, want: ""},
		{name: "long/0", in: long, maxLen: 0, want: ""},
		{name: "long/1", in: long, maxLen: 1, want: "a"},
		{name: "long/2", in: long, maxLen: 2, want: "ab"},
		{name: "long/3", in: long, maxLen: 3, want: "..."},
		{name: "long/4", in: long, maxLen: 4, want: "a..."},

		{name: "multibyte/-1", in: multibyte, maxLen: -1, want: ""},
		{name: "multibyte/0", in: multibyte, maxLen: 0, want: ""},
		{name: "multibyte/1", in: multibyte, maxLen: 1, want: "안"},
		{name: "multibyte/2", in: multibyte, maxLen: 2, want: "안녕"},
		{name: "multibyte/3", in: multibyte, maxLen: 3, want: "..."},
		{name: "multibyte/4", in: multibyte, maxLen: 4, want: "안..."},
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
	*got = got.Add(time.Hour)
	if !local.Equal(time.Date(2026, 6, 10, 12, 0, 0, 0, loc)) {
		t.Error("mutating clone affected the source")
	}
}
