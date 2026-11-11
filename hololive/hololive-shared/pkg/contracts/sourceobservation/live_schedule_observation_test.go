package sourceobservation

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestLiveScheduleObservationTimeMustPrecedeTheSlot(t *testing.T) {
	statuses := slices.Clone(liveSnapshotQueryStatuses)
	scheduled := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	envelopeFor := func(session LiveSessionV1, generation int64) Envelope {
		payload := LiveSnapshotV1{
			Sessions: []LiveSessionV1{session},
			Coverage: GlobalChannelCoverageV1{RequestedChannelIDs: []string{testChannelID}, Filters: LiveFiltersV1{Statuses: statuses}},
			Query:    &LiveSnapshotQueryV1{ChannelID: testChannelID, Source: "streams", Statuses: statuses, Exhausted: true, PageCount: 1},
		}

		if generation == LiveSnapshotMetadataContractGeneration {
			payload.Query = nil
		}

		envelope := newPaginatedEnvelope(t, KindLiveSnapshot, mustMarshalPayload(t, payload), CompletenessComplete)

		envelope.ContractGeneration = generation

		return envelope
	}
	slot := envelopeFor(LiveSessionV1{}, LiveSnapshotQueryContractGeneration).ScheduledFor
	upcoming := func(observed time.Time) LiveSessionV1 {
		return LiveSessionV1{
			VideoID: testVideoID, ChannelID: testChannelID, Status: queryUpcomingStatus,
			ScheduledAt: new(scheduled), ScheduleObservedAt: new(observed),
		}
	}

	if _, err := PrepareEnvelope(envelopeFor(upcoming(slot.Add(-2*time.Minute)), LiveSnapshotQueryContractGeneration)); err != nil {
		t.Fatalf("earlier slot observation rejected: %v", err)
	}

	live := upcoming(slot.Add(-2 * time.Minute))

	live.Status = testStatusLive

	for name, envelope := range map[string]Envelope{
		"same slot":           envelopeFor(upcoming(slot), LiveSnapshotQueryContractGeneration),
		"later slot":          envelopeFor(upcoming(slot.Add(time.Second)), LiveSnapshotQueryContractGeneration),
		"beyond reuse window": envelopeFor(upcoming(slot.Add(-LiveScheduleObservationMaxAge-time.Second)), LiveSnapshotQueryContractGeneration),
		"live row":            envelopeFor(live, LiveSnapshotQueryContractGeneration),
		"metadata generation": envelopeFor(upcoming(slot.Add(-2*time.Minute)), LiveSnapshotMetadataContractGeneration),
	} {
		if _, err := PrepareEnvelope(envelope); err == nil || !strings.Contains(err.Error(), "schedule observation time") {
			t.Fatalf("%s: error = %v, want schedule observation rejection", name, err)
		}
	}
}
