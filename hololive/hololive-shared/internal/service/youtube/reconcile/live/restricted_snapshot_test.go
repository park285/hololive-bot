package live

import (
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func TestPartialSnapshotPreservesRestrictedSessionWhileApplyingKnownLiveAndEnd(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.September, 11, 3, 0, 0, 0, time.UTC)
	restricted := sessionFact(testLiveStatus)

	restricted.VideoID = "restricted-video"

	ending := sessionFact(testLiveStatus)

	ending.VideoID = "ending-video"

	upcoming := sessionFact("UPCOMING")

	upcoming.VideoID = "starting-video"
	upcoming.ScheduledAt = new(at.Add(time.Hour))

	initial := liveEvidence(1, at, contract.CompletenessComplete, contract.ContinuityNotApplicable, restricted, ending, upcoming)

	ending.Status = "ENDED"
	ending.LiveStartConfirmed = false
	upcoming.Status = testLiveStatus
	upcoming.LiveStartConfirmed = true

	partial := liveEvidence(2, at.Add(time.Hour), contract.CompletenessPartial, contract.ContinuityNotApplicable, ending, upcoming)
	result := mustReduceAll(t, emptyState(), []Evidence{initial, partial}, 0)
	sessions := make(map[string]SessionState, len(result.Sessions))

	for _, session := range result.Sessions {
		sessions[session.VideoID] = session
	}

	if sessions["starting-video"].Status != StatusLive || sessions["ending-video"].Status != StatusEnded {
		t.Fatalf("known transitions were blocked: %#v", sessions)
	}

	retained := sessions["restricted-video"]
	if retained.Status != StatusLive || !retained.LastSeenAt.Equal(at) || retained.Clock.ConsecutiveAbsenceSlots != 0 {
		t.Fatalf("restricted session was refreshed or ended without evidence: %#v", retained)
	}
}
