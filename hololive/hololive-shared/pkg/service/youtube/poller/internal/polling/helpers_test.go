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

package polling

import (
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

func TestIsLiveReplayVideo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want bool
	}{
		{name: "streamed keyword", text: "Streamed 2 hours ago", want: true},
		{name: "premiered keyword", text: "PREMIERED 1 day ago", want: true},
		{name: "normal upload", text: "Uploaded 3 hours ago", want: false},
		{name: "empty text", text: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsLiveReplayVideo(tt.text); got != tt.want {
				t.Fatalf("IsLiveReplayVideo(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestConvertThumbnails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   []scraper.Thumbnail
		want domain.ThumbnailsJSON
	}{
		{
			name: "empty input",
			in:   nil,
			want: nil,
		},
		{
			name: "maps thumbnail entries",
			in: []scraper.Thumbnail{
				{URL: "https://a", Width: 120, Height: 90},
				{URL: "https://b", Width: 640, Height: 480},
			},
			want: domain.ThumbnailsJSON{
				{URL: "https://a", Width: 120, Height: 90},
				{URL: "https://b", Width: 640, Height: 480},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ConvertThumbnails(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("ConvertThumbnails() len = %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("ConvertThumbnails()[%d] = %#v, want %#v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestMustMarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		value     any
		wantExact string
		contains  []string
	}{
		{
			name:      "marshal success",
			value:     map[string]any{"name": "pekora", "value": 1},
			wantExact: "",
			contains:  []string{"\"name\":\"pekora\"", "\"value\":1"},
		},
		{
			name:      "marshal failure returns fallback",
			value:     map[string]any{"fn": func() {}},
			wantExact: "{}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := MustMarshalJSON(tt.value)
			if tt.wantExact != "" && got != tt.wantExact {
				t.Fatalf("MustMarshalJSON() = %q, want %q", got, tt.wantExact)
			}
			for _, needle := range tt.contains {
				if !strings.Contains(got, needle) {
					t.Fatalf("MustMarshalJSON() = %q, expected substring %q", got, needle)
				}
			}
		})
	}
}

func TestParseViewerCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want int
	}{
		{name: "empty", text: "", want: 0},
		{name: "comma separated", text: "12,345 watching", want: 12345},
		{name: "k suffix", text: "1.2K viewers", want: 1200},
		{name: "m suffix", text: "3.45m waiting", want: 3450000},
		{name: "non numeric", text: "watching now", want: 0},
		{name: "trim spaces", text: "  999 waiting  ", want: 999},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ParseViewerCount(tt.text); got != tt.want {
				t.Fatalf("ParseViewerCount(%q) = %d, want %d", tt.text, got, tt.want)
			}
		})
	}
}
