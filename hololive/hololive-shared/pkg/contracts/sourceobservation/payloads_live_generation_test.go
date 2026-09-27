package sourceobservation

import (
	"strings"
	"testing"
)

// 계획 T11 C6(stack-audit 2026-09-26)에서 live_snapshot generation 1 decoder를 지웠다. 남은 generation 1 관측은 조용히
// 받아들이지 않고 unsupported로 드러나야 하며, generation 2는 그대로 받는다.
func TestLiveSnapshotRejectsRetiredGenerationOne(t *testing.T) {
	payload := mustMarshalPayload(t, LiveSnapshotV1{
		Sessions: []LiveSessionV1{{VideoID: testVideoID, ChannelID: testChannelID, Status: testStatusLive}},
		Coverage: GlobalChannelCoverageV1{
			RequestedChannelIDs: []string{testChannelID},
			Filters:             LiveFiltersV1{Statuses: []string{testStatusLive}},
		},
	})
	envelope := newPaginatedEnvelope(t, KindLiveSnapshot, payload, CompletenessComplete)

	envelope.ContractGeneration = 1
	if _, err := PrepareEnvelope(envelope); err == nil || !strings.Contains(err.Error(), "unsupported live snapshot contract generation 1") {
		t.Fatalf("PrepareEnvelope(generation 1) error = %v, want unsupported generation", err)
	}

	envelope.ContractGeneration = LiveSnapshotMetadataContractGeneration
	if _, err := PrepareEnvelope(envelope); err != nil {
		t.Fatalf("PrepareEnvelope(generation 2) error = %v", err)
	}
}
