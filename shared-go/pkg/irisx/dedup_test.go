package irisx

import "testing"

func TestDedupKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "normal",
			in:   "abc-123",
			want: "iris:msg:{abc-123}",
		},
		{
			name: "trim spaces",
			in:   "  id-1  ",
			want: "iris:msg:{id-1}",
		},
		{
			name: "empty",
			in:   "",
			want: "",
		},
		{
			name: "spaces only",
			in:   "   ",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := DedupKey(tt.in)
			if got != tt.want {
				t.Fatalf("DedupKey(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
