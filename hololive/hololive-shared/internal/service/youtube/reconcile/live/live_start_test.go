package live

import (
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func TestReduceUnconfirmedLivePreservesUpcomingState(t *testing.T) {
	t.Parallel()

	upcoming := upcomingA()

	first, err := Reduce(*emptyState(), upcoming, 0, upcoming.ReceivedAt)
	if err != nil {
		t.Fatalf("reduce upcoming: %v", err)
	}

	state := stateFromDecision(emptyState(), &first)
	at := upcoming.EffectiveAt.Add(time.Minute)
	fact := sessionFact(testLiveStatus)

	fact.LiveStartConfirmed = false
	fact.Title = "updated waiting room"
	fact.StartedAt = copyTime(upcoming.EffectiveAt.Add(30 * time.Second))

	unconfirmed := liveEvidence(
		2, at, contract.CompletenessComplete, contract.ContinuityContiguous, fact,
	)

	decision, err := Reduce(state, unconfirmed, 0, at)
	if err != nil {
		t.Fatalf("reduce unconfirmed live: %v", err)
	}

	session := sessionOf(&decision)
	if session.Status != StatusUpcoming {
		t.Fatalf("status = %s, want UPCOMING", session.Status)
	}

	if session.Title != fact.Title || !session.LastSeenAt.Equal(at) || !session.Present {
		t.Fatalf("presence fields not merged: %#v", session)
	}

	if session.StartedAt != nil || session.LiveFirstSeenAt != nil || session.Clock.LastLivePositiveAt != nil ||
		session.Clock.LastLivePositiveSeenAt != nil {
		t.Fatalf("unconfirmed live advanced start state: %#v", session)
	}

	if session.Clock.LastUpcomingPositiveAt == nil ||
		!session.Clock.LastUpcomingPositiveAt.Equal(upcoming.EffectiveAt) {
		t.Fatalf("upcoming clock changed: %#v", session.Clock)
	}

	if len(decision.Applications) != 1 || decision.Applications[0].Decision != "LIVE_START_UNCONFIRMED" {
		t.Fatalf("applications = %#v", decision.Applications)
	}
}

func TestReduceUnconfirmedAndConfirmedLiveConvergeByEvidenceTime(t *testing.T) {
	t.Parallel()

	t1 := time.Date(2026, time.August, 29, 9, 14, 0, 0, time.UTC)
	t2 := t1.Add(time.Minute)
	unconfirmedFact := sessionFact(testLiveStatus)

	unconfirmedFact.LiveStartConfirmed = false

	unconfirmed := liveEvidence(
		1, t1, contract.CompletenessComplete, contract.ContinuityContiguous, unconfirmedFact,
	)
	confirmed := liveEvidence(
		2, t2, contract.CompletenessComplete, contract.ContinuityContiguous, sessionFact(testLiveStatus),
	)

	for _, order := range [][]Evidence{{unconfirmed, confirmed}, {confirmed, unconfirmed}} {
		decision := mustReduceAll(t, emptyState(), order, 0)
		session := sessionOf(decision)

		if session.Status != StatusLive {
			t.Fatalf("status = %s, want LIVE", session.Status)
		}

		if session.StartedAt == nil || !session.StartedAt.Equal(t2) {
			t.Fatalf("started at = %v, want %s", session.StartedAt, t2)
		}

		if session.Clock.LastLivePositiveAt == nil || !session.Clock.LastLivePositiveAt.Equal(t2) {
			t.Fatalf("last live positive = %v, want %s", session.Clock.LastLivePositiveAt, t2)
		}
	}
}

func TestReduceUnconfirmedLiveClearsEndCandidateWithoutChangingStart(t *testing.T) {
	t.Parallel()

	started := time.Date(2026, time.August, 29, 8, 0, 0, 0, time.UTC)
	endEvidenceAt := started.Add(time.Hour)
	observationID := int64(41)
	kind := EndEvidenceScopedAbsence
	nextCheck := endEvidenceAt.Add(time.Hour)
	state := State{
		Sessions: map[string]SessionState{
			testVideoID: {
				VideoID: testVideoID, ChannelID: "UC_TEST", Status: StatusLive,
				StartedAt: &started, LiveFirstSeenAt: &started, LastSeenAt: started,
				Clock: LiveEvidenceClock{
					LastLivePositiveAt: &started, LastLivePositiveSeenAt: &started,
					LastEndEvidenceAt: &endEvidenceAt, EndCandidateKind: &kind,
					EndCandidateObservationID: &observationID, NextEndCheckAt: &nextCheck,
				},
			},
		},
		PendingEnds: map[string]PendingEnd{
			testVideoID: {VideoID: testVideoID, ObservationID: observationID, EffectiveAt: endEvidenceAt},
		},
	}
	fact := sessionFact(testLiveStatus)

	fact.LiveStartConfirmed = false

	at := nextCheck.Add(time.Minute)
	evidence := liveEvidence(
		42, at, contract.CompletenessComplete, contract.ContinuityContiguous, fact,
	)

	decision, err := Reduce(state, evidence, time.Hour, at)
	if err != nil {
		t.Fatalf("reduce unconfirmed live: %v", err)
	}

	session := sessionOf(&decision)
	if session.Status != StatusLive || session.StartedAt == nil || !session.StartedAt.Equal(started) {
		t.Fatalf("confirmed start changed: %#v", session)
	}

	if session.Clock.LastLivePositiveAt == nil || !session.Clock.LastLivePositiveAt.Equal(started) {
		t.Fatalf("live clock changed: %#v", session.Clock)
	}

	if session.Clock.EndCandidateKind != nil || len(decision.PendingEnds) != 0 {
		t.Fatalf("end candidate retained: session=%#v pending=%#v", session, decision.PendingEnds)
	}
}

func TestReduceUnconfirmedLiveDoesNotReopenEndedSession(t *testing.T) {
	t.Parallel()

	unconfirmed := lateLiveA()

	unconfirmed.Sessions[0].LiveStartConfirmed = false

	decision := mustReduceAll(t, emptyState(), []Evidence{upcomingA(), liveA(), endA(), unconfirmed}, 0)

	if session := sessionOf(decision); session.Status != StatusEnded {
		t.Fatalf("status = %s, want ENDED", session.Status)
	}
}
