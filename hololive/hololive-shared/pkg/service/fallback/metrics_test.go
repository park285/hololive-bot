package fallback

import "testing"

func TestPrimaryOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		attempted int
		succeeded int
		failed    int
		want      string
	}{
		{name: "skipped", attempted: 0, succeeded: 0, failed: 0, want: "skipped"},
		{name: "success", attempted: 3, succeeded: 3, failed: 0, want: "success"},
		{name: "partial", attempted: 3, succeeded: 2, failed: 1, want: "partial"},
		{name: "empty", attempted: 3, succeeded: 0, failed: 0, want: "empty"},
		{name: "failed", attempted: 3, succeeded: 0, failed: 3, want: "failed"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := primaryOutcome(tt.attempted, tt.succeeded, tt.failed); got != tt.want {
				t.Fatalf("primaryOutcome() = %q, want %q", got, tt.want)
			}
		})
	}
}
