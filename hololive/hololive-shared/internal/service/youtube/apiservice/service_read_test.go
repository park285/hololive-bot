package apiservice

import (
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
	"testing"
)

func TestRecentScraperVideoIDs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		videos []*parser.Video
		want   []string
	}{
		{
			name:   "nil slice yields empty non-nil slice",
			videos: nil,
			want:   []string{},
		},
		{
			name:   "empty slice yields empty non-nil slice",
			videos: []*parser.Video{},
			want:   []string{},
		},
		{
			name: "preserves order of video IDs",
			videos: []*parser.Video{
				{VideoID: "aaa"},
				{VideoID: "bbb"},
				{VideoID: "ccc"},
			},
			want: []string{"aaa", "bbb", "ccc"},
		},
		{
			name: "passes through empty video IDs verbatim",
			videos: []*parser.Video{
				{VideoID: ""},
				{VideoID: "xyz"},
			},
			want: []string{"", "xyz"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := recentScraperVideoIDs(tt.videos)
			if got == nil {
				t.Fatalf("recentScraperVideoIDs(%v) = nil, want non-nil slice", tt.videos)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("recentScraperVideoIDs(%v) len = %d, want %d (got %v)", tt.videos, len(got), len(tt.want), got)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("recentScraperVideoIDs(%v)[%d] = %q, want %q", tt.videos, i, got[i], tt.want[i])
				}
			}
		})
	}
}
