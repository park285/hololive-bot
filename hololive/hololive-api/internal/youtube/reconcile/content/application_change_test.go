package content

import (
	"testing"
	"time"
)

func TestReduceRecordsVideoApplicationOnlyOnStateChange(t *testing.T) {
	t.Parallel()

	renamed := videoA()

	renamed.Title = "Renamed"

	replay := positiveA()

	replay.ObservationID = 9

	cases := []struct {
		name     string
		evidence []Evidence
		want     []string
	}{
		{name: "first positive", evidence: []Evidence{positiveA()}, want: []string{"APPLIED"}},
		{name: "newer unchanged positive", evidence: []Evidence{positiveA(), latePositiveA()}},
		{name: "equal time replay", evidence: []Evidence{positiveA(), replay}},
		{
			name:     "newer changed positive",
			evidence: []Evidence{positiveA(), positiveAt(4, time.Date(2026, time.August, 14, 4, 0, 0, 0, time.UTC), renamed)},
			want:     []string{"APPLIED"},
		},
		{name: "newer positive clears missing", evidence: []Evidence{positiveA(), completeNegativeB(), latePositiveA()}, want: []string{"APPLIED"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := videoDecisions(mustReduceAll(t, seededState(), tc.evidence, 0))
			if len(got) != len(tc.want) {
				t.Fatalf("video decisions = %v, want %v", got, tc.want)
			}

			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("video decisions = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func videoDecisions(decision *Decision) []string {
	var out []string

	for _, app := range decision.Applications {
		if app.EntityKind == entityKindVideo {
			out = append(out, app.Decision)
		}
	}

	return out
}
