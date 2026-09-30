package live

import (
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func TestLifecycleOriginAbsenceCannotStartTracking(t *testing.T) {
	t.Parallel()

	for _, origin := range []LifecycleOrigin{OriginMetadataOnly, OriginLegacyUnknown} {
		for _, status := range []Status{StatusUpcoming, StatusLive} {
			t.Run(string(origin)+"/"+string(status), func(t *testing.T) {
				t.Parallel()

				state := emptyState()

				state.Sessions[testVideoID] = SessionState{
					VideoID: testVideoID, ChannelID: channelCoverage().RequestedChannelIDs[0], Status: status,
					LifecycleOrigin: origin, Present: true,
					LastSeenAt: time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC),
				}

				for _, evidence := range []Evidence{firstAbsence(), secondAbsence(0)} {
					decision, err := Reduce(*state, evidence, 0, evidence.ReceivedAt)
					if err != nil {
						t.Fatal(err)
					}

					if len(decision.Sessions) != 0 || len(decision.PendingEnds) != 0 {
						t.Fatalf("absence started tracking: %#v", decision)
					}
				}
			})
		}
	}
}

func TestLifecycleOriginPositivePromotesMetadataAndLegacy(t *testing.T) {
	t.Parallel()

	for _, origin := range []LifecycleOrigin{OriginMetadataOnly, OriginLegacyUnknown} {
		for _, evidence := range []Evidence{upcomingA(), liveA()} {
			t.Run(string(origin)+"/"+evidence.Sessions[0].Status, func(t *testing.T) {
				t.Parallel()

				state := emptyState()

				state.Sessions[testVideoID] = SessionState{
					VideoID: testVideoID, ChannelID: channelCoverage().RequestedChannelIDs[0], Status: StatusUpcoming,
					LifecycleOrigin: origin, Present: true,
				}

				decision, err := Reduce(*state, evidence, 0, evidence.ReceivedAt)
				if err != nil {
					t.Fatal(err)
				}

				if len(decision.Sessions) != 1 || decision.Sessions[0].LifecycleOrigin != OriginObserved {
					t.Fatalf("positive did not promote origin: %#v", decision.Sessions)
				}

				if evidence.Sessions[0].Status == string(StatusUpcoming) && decision.Sessions[0].Clock.LastLivePositiveAt != nil {
					t.Fatal("upcoming positive invented a LIVE clock")
				}
			})
		}
	}
}

func TestLifecycleOriginUnconfirmedLiveRemainsMetadata(t *testing.T) {
	t.Parallel()

	fact := sessionFact(string(StatusLive))

	fact.LiveStartConfirmed = false

	evidence := liveEvidence(1, liveA().ReceivedAt, contract.CompletenessPartial, contract.ContinuityContiguous, fact)

	decision, err := Reduce(*emptyState(), evidence, 0, evidence.ReceivedAt)
	if err != nil {
		t.Fatal(err)
	}

	if len(decision.Sessions) != 1 || decision.Sessions[0].LifecycleOrigin != OriginMetadataOnly ||
		decision.Sessions[0].Clock.LastLivePositiveAt != nil || decision.Sessions[0].Clock.LastUpcomingPositiveAt != nil {
		t.Fatalf("unconfirmed LIVE claimed observed origin: %#v", decision.Sessions)
	}
}
