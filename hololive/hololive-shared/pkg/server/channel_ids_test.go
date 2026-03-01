package server

import "testing"

func TestSplitChannelIDs(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{
			name: "single",
			raw:  "UC123",
			want: []string{"UC123"},
		},
		{
			name: "multiple",
			raw:  "UC123,UC456,UC789",
			want: []string{"UC123", "UC456", "UC789"},
		},
		{
			name: "spaces and tabs",
			raw:  " UC123,\tUC456 , UC789 ",
			want: []string{"UC123", "UC456", "UC789"},
		},
		{
			name: "empty and commas",
			raw:  ",,UC123,,",
			want: []string{"UC123"},
		},
		{
			name: "empty input",
			raw:  "",
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SplitChannelIDs(tt.raw)
			if len(got) != len(tt.want) {
				t.Fatalf("SplitChannelIDs(%q) len=%d want=%d got=%v", tt.raw, len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("SplitChannelIDs(%q)[%d]=%q want=%q", tt.raw, i, got[i], tt.want[i])
				}
			}
		})
	}
}
