package sourceobservation

import (
	"strings"
	"testing"
)

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
