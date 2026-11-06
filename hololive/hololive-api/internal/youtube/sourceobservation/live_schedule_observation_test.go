package sourceobservation

import (
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

// 재사용 일정의 원래 관측 슬롯이 reducer 입력까지 전달되어야 옛 일정이 더 새 관측을 덮지 않는다.
func TestLiveEvidenceCarriesReusedScheduleObservationTime(t *testing.T) {
	t.Parallel()

	slot := time.Date(2026, time.November, 6, 1, 4, 0, 0, time.UTC)
	reusedAt := slot.Add(-4 * time.Minute)
	scheduled := slot.Add(10 * time.Hour)

	payload, err := contract.MarshalPayloadV1(contract.LiveSnapshotV1{
		Sessions: []contract.LiveSessionV1{{
			VideoID: testVideoID, ChannelID: testChannelID, Status: "UPCOMING",
			ScheduledAt: new(scheduled), ScheduleObservedAt: new(reusedAt),
		}},
		Coverage: contract.GlobalChannelCoverageV1{
			RequestedChannelIDs: []string{testChannelID},
			Filters:             contract.LiveFiltersV1{Statuses: []string{"UPCOMING"}},
		},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	evidence, err := liveEvidenceFromObservation(&Observation{
		Provider: contract.ProviderYouTubeJS, ObservationKind: contract.KindLiveSnapshot,
		ID: 1, ScheduledFor: slot, EffectiveAt: slot, ReceivedAt: slot, Payload: payload,
	})
	if err != nil {
		t.Fatalf("live evidence: %v", err)
	}

	if got := evidence.Sessions[0].ScheduleObservedAt; got == nil || !got.Equal(reusedAt) {
		t.Fatalf("schedule observed at = %v, want %v", got, reusedAt)
	}
}
