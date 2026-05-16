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

package model

import (
	"testing"
	"time"

	sharedtime "github.com/kapu/hololive-shared/pkg/util"
)

func TestStreamStatus_IsValid(t *testing.T) {
	tests := []struct {
		name   string
		status StreamStatus
		want   bool
	}{
		{"live is valid", StreamStatusLive, true},
		{"upcoming is valid", StreamStatusUpcoming, true},
		{"past is valid", StreamStatusPast, true},
		{"invalid status", StreamStatus("invalid"), false},
		{"empty status", StreamStatus(""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.IsValid(); got != tt.want {
				t.Errorf("StreamStatus.IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStream_MinutesUntilStart(t *testing.T) {
	now := time.Now()
	// 내림이므로 정확한 분 경계 테스트 시 30초 여유를 추가하여 테스트 실행 지연의 영향을 제거
	future := now.Add(10*time.Minute + 30*time.Second)
	futureBoundary := now.Add(4*time.Minute + 30*time.Second)
	futureFloorEdge := now.Add(5*time.Minute + 59*time.Second)
	past := now.Add(-10 * time.Minute)

	tests := []struct {
		name   string
		stream *Stream
		want   int
	}{
		{
			name:   "no start time",
			stream: &Stream{StartScheduled: nil},
			want:   -1,
		},
		{
			name:   "future start",
			stream: &Stream{StartScheduled: &future},
			want:   10,
		},
		{
			name:   "future start rounds down",
			stream: &Stream{StartScheduled: &futureBoundary},
			want:   4,
		},
		{
			name:   "5min 59sec floors to 5",
			stream: &Stream{StartScheduled: &futureFloorEdge},
			want:   5,
		},
		{
			name:   "past start",
			stream: &Stream{StartScheduled: &past},
			want:   -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.stream.MinutesUntilStart()
			if got != tt.want {
				t.Errorf("Stream.MinutesUntilStart() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStream_MinutesUntilStartUsesSharedTimeSemantics(t *testing.T) {
	now := time.Now()
	future := now.Add(7*time.Minute + 20*time.Second)
	past := now.Add(-1 * time.Minute)

	tests := []struct {
		name   string
		target *time.Time
	}{
		{name: "nil target", target: nil},
		{name: "past target", target: &past},
		{name: "future target", target: &future},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stream := &Stream{StartScheduled: tt.target}
			got := stream.MinutesUntilStart()
			want := sharedtime.MinutesUntilFloorPtr(tt.target, time.Now())
			if tt.target != nil {
				want = sharedtime.MinutesUntilFloorPtr(tt.target, time.Now())
			}
			if got != want {
				t.Fatalf("MinutesUntilStart() = %d, want %d", got, want)
			}
		})
	}
}

func TestStream_GetYouTubeURL(t *testing.T) {
	customLink := "https://youtube.com/watch?v=custom123"

	tests := []struct {
		name   string
		stream *Stream
		want   string
	}{
		{
			name:   "with custom link",
			stream: &Stream{ID: "abc123", Link: &customLink},
			want:   customLink,
		},
		{
			name:   "without link",
			stream: &Stream{ID: "abc123", Link: nil},
			want:   "https://youtube.com/watch?v=abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.stream.GetYouTubeURL(); got != tt.want {
				t.Errorf("Stream.GetYouTubeURL() = %v, want %v", got, tt.want)
			}
		})
	}
}
