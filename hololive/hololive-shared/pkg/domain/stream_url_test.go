// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package domain_test

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestChzzkLiveURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		channelID string
		want      string
	}{
		{
			name:      "channelID 있음",
			channelID: "abc123",
			want:      "https://chzzk.naver.com/live/abc123",
		},
		{
			name:      "빈 channelID는 passthrough",
			channelID: "",
			want:      "https://chzzk.naver.com/live/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := domain.ChzzkLiveURL(tt.channelID); got != tt.want {
				t.Errorf("ChzzkLiveURL(%q) = %q, want %q", tt.channelID, got, tt.want)
			}
		})
	}
}

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
