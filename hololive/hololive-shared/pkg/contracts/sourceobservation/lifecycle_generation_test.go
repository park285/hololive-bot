package sourceobservation

import (
	"strings"
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

func TestVideoLifecycleRequiresCurrentSchemaAndGeneration(t *testing.T) {
	envelope := newLiveCheckEnvelope(t, KindVideoLiveCheck, publicEndedVideoCheck(), CompletenessPartial)
	if _, err := PrepareEnvelope(envelope); err != nil {
		t.Fatal(err)
	}

	for _, version := range []struct {
		schema     int16
		generation int64
	}{{1, 1}, {1, 2}, {2, 1}, {2, 3}} {
		invalid := envelope

		invalid.SchemaVersion, invalid.ContractGeneration = version.schema, version.generation

		if _, err := PrepareEnvelope(invalid); err == nil {
			t.Fatalf("unsupported video lifecycle schema=%d generation=%d accepted", version.schema, version.generation)
		}
	}
}

// 저장된 퇴역 generation 1 video_live_check는 consume 전 검증의 decode 단계에서 거부된다.
// API consumer는 이 거부에 기대어 video_live_check 세대를 다시 확인하지 않는다.
func TestStoredRetiredVideoLiveCheckRejectedAtDecode(t *testing.T) {
	prepared, err := PrepareEnvelope(newLiveCheckEnvelope(t, KindVideoLiveCheck, publicEndedVideoCheck(), CompletenessPartial))
	if err != nil {
		t.Fatal(err)
	}

	stored := prepared

	stored.SchemaVersion, stored.ContractGeneration = SchemaVersionV1, 1

	_, err = stored.ValidateAndCanonicalPayload()
	if err == nil || !strings.Contains(err.Error(), "unsupported video live check contract generation 1") {
		t.Fatalf("ValidateAndCanonicalPayload(schema 1, generation 1) error = %v, want decode rejection", err)
	}
}
