package youtube

import "testing"

func TestShouldReturnFallbackError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		currentResults    int
		failedTargets     int
		fallbackSuccesses int
		want              bool
	}{
		{
			name:              "returns true when all primary targets failed and fallback had no successes",
			currentResults:    0,
			failedTargets:     3,
			fallbackSuccesses: 0,
			want:              true,
		},
		{
			name:              "returns false when partial primary results exist",
			currentResults:    2,
			failedTargets:     3,
			fallbackSuccesses: 0,
			want:              false,
		},
		{
			name:              "returns false when fallback succeeded but no items were produced",
			currentResults:    0,
			failedTargets:     3,
			fallbackSuccesses: 1,
			want:              false,
		},
		{
			name:              "returns false when there were no failed targets",
			currentResults:    0,
			failedTargets:     0,
			fallbackSuccesses: 0,
			want:              false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := shouldReturnFallbackError(tt.currentResults, tt.failedTargets, tt.fallbackSuccesses)
			if got != tt.want {
				t.Fatalf("shouldReturnFallbackError(%d, %d, %d) = %v, want %v",
					tt.currentResults, tt.failedTargets, tt.fallbackSuccesses, got, tt.want)
			}
		})
	}
}
