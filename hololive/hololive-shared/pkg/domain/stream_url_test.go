package domain_test

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestYouTubeWatchURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		videoID string
		want    string
	}{
		{
			name:    "videoID 있음",
			videoID: "abc123",
			want:    "https://youtube.com/watch?v=abc123",
		},
		{
			name:    "빈 videoID는 passthrough",
			videoID: "",
			want:    "https://youtube.com/watch?v=",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := domain.YouTubeWatchURL(tt.videoID); got != tt.want {
				t.Errorf("YouTubeWatchURL(%q) = %q, want %q", tt.videoID, got, tt.want)
			}
		})
	}
}
