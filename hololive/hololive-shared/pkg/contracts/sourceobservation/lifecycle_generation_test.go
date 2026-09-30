package sourceobservation

import (
	"testing"
)

const queryUpcomingStatus = "UPCOMING"

func TestLiveQueryGenerationRequiresProofForCompleteCoverage(t *testing.T) {
	for _, limited := range []bool{false, true} {
		payload := LiveSnapshotV1{
			Sessions: []LiveSessionV1{},
			Coverage: GlobalChannelCoverageV1{RequestedChannelIDs: []string{testChannelID}, Filters: LiveFiltersV1{Statuses: []string{"ENDED", "LIVE", queryUpcomingStatus}}},
			Query:    &LiveSnapshotQueryV1{ChannelID: testChannelID, Source: "streams", Statuses: []string{"ENDED", "LIVE", queryUpcomingStatus}, Exhausted: !limited, PageCount: 1},
		}
		envelope := newPaginatedEnvelope(t, KindLiveSnapshot, mustMarshalPayload(t, payload), CompletenessComplete)

		envelope.ContractGeneration = LiveSnapshotQueryContractGeneration

		_, err := PrepareEnvelope(envelope)
		if (err != nil) != limited {
			t.Fatalf("limited=%t complete query error=%v", limited, err)
		}

		envelope.Completeness = CompletenessPartial
		if _, err := PrepareEnvelope(envelope); err != nil {
			t.Fatal(err)
		}

		envelope.ContractGeneration = LiveSnapshotMetadataContractGeneration
		if _, err := PrepareEnvelope(envelope); err == nil {
			t.Fatal("new query proof must not reinterpret a legacy observation")
		}
	}
}
