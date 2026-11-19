package domain

import (
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/timeutil"
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
			want := timeutil.MinutesUntilFloorPtr(tt.target, time.Now())

			if tt.target != nil {
				want = timeutil.MinutesUntilFloorPtr(tt.target, time.Now())
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
