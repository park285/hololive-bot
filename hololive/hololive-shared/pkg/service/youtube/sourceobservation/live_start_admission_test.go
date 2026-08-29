package sourceobservation

import (
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/reconcile/live"
)

func TestLiveEvidenceHolodexLiveWithoutActualStartDoesNotAdvance(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.August, 29, 9, 14, 44, 0, time.UTC)

	payload, err := contract.MarshalPayloadV1(contract.LiveSnapshotV1{
		Sessions: []contract.LiveSessionV1{{
			VideoID: testVideoID, ChannelID: testChannelID, Status: testStatusLive,
		}},
		Coverage: contract.GlobalChannelCoverageV1{
			RequestedChannelIDs: []string{testChannelID},
			Filters:             contract.LiveFiltersV1{Statuses: []string{"UPCOMING", testStatusLive}},
		},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	evidence, err := liveEvidenceFromObservation(&Observation{
		Provider: contract.ProviderHolodex, ObservationKind: contract.KindLiveSnapshot,
		ID: 1, ScheduledFor: at, EffectiveAt: at, ReceivedAt: at, Payload: payload,
	})
	if err != nil {
		t.Fatalf("live evidence: %v", err)
	}

	if evidence.Sessions[0].LiveStartConfirmed {
		t.Fatal("Holodex LIVE without actual start must remain unconfirmed")
	}

	decision, err := live.Reduce(live.State{
		Sessions: map[string]live.SessionState{}, PendingEnds: map[string]live.PendingEnd{},
	}, evidence, 0, at)
	if err != nil {
		t.Fatalf("reduce: %v", err)
	}

	if len(decision.Sessions) != 1 {
		t.Fatalf("session count = %d, want 1", len(decision.Sessions))
	}

	if decision.Sessions[0].Status != live.StatusUpcoming {
		t.Fatalf("status = %s, want UPCOMING", decision.Sessions[0].Status)
	}

	if decision.Sessions[0].StartedAt != nil || decision.Sessions[0].LiveFirstSeenAt != nil ||
		decision.Sessions[0].Clock.LastLivePositiveAt != nil {
		t.Fatalf("unconfirmed live advanced start state: %#v", decision.Sessions[0])
	}
}

func TestLiveEvidenceDerivesStartConfirmation(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.August, 29, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		provider  contract.Provider
		startedAt *time.Time
		want      bool
	}{
		{name: "YouTube direct live", provider: contract.ProviderYouTubeJS, want: true},
		{name: "Holodex nullable actual", provider: contract.ProviderHolodex},
		{name: "Holodex actual start", provider: contract.ProviderHolodex, startedAt: &at, want: true},
		{name: "unsupported provider", provider: contract.ProviderHololiveOfficial},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			payload, err := contract.MarshalPayloadV1(contract.LiveSnapshotV1{
				Sessions: []contract.LiveSessionV1{{
					VideoID: testVideoID, ChannelID: testChannelID, Status: testStatusLive, StartedAt: test.startedAt,
				}},
				Coverage: contract.GlobalChannelCoverageV1{
					RequestedChannelIDs: []string{testChannelID},
					Filters:             contract.LiveFiltersV1{Statuses: []string{testStatusLive}},
				},
			})
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}

			evidence, err := liveEvidenceFromObservation(&Observation{
				Provider: test.provider, ObservationKind: contract.KindLiveSnapshot,
				ID: 1, ScheduledFor: at, EffectiveAt: at, ReceivedAt: at, Payload: payload,
			})
			if err != nil {
				t.Fatalf("live evidence: %v", err)
			}

			if evidence.Sessions[0].LiveStartConfirmed != test.want {
				t.Fatalf("confirmed = %t, want %t", evidence.Sessions[0].LiveStartConfirmed, test.want)
			}
		})
	}
}
