package sourceobservation

import (
	"testing"
	"time"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func TestRelocatedScheduleObservationRoundTripsThroughStorage(t *testing.T) {
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	ctx := t.Context()
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindLiveSnapshot, "youtubejs_channel_live")
	statuses := []string{testStatusEnded, testStatusLive, "UPCOMING"}

	raw, err := contract.MarshalPayloadV1(contract.LiveSnapshotV1{
		Sessions: []contract.LiveSessionV1{{
			VideoID: testVideoID, ChannelID: testChannelID, Status: "UPCOMING",
			ScheduledAt: new(proof.ScheduledFor.Add(3 * time.Hour)),
		}},
		Coverage: contract.GlobalChannelCoverageV1{RequestedChannelIDs: []string{testChannelID}, Filters: contract.LiveFiltersV1{Statuses: statuses}},
		Query:    &contract.LiveSnapshotQueryV1{ChannelID: testChannelID, Source: "streams", Statuses: statuses, Exhausted: true, PageCount: 1},
	})
	if err != nil {
		t.Fatal(err)
	}

	observed := proof.ScheduledFor.Add(-90*time.Second + 123*time.Millisecond)

	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider: contract.ProviderYouTubeJS, ObservationKind: contract.KindLiveSnapshot, SubjectKey: testChannelID,
		SchemaVersion: contract.SchemaVersionV1, ContractGeneration: contract.LiveSnapshotQueryContractGeneration,
		ScheduledFor: proof.ScheduledFor, ObservedAt: proof.ScheduledFor.Add(time.Second),
		Completeness: contract.CompletenessComplete, Continuity: contract.ContinuityNotApplicable,
		Payload: raw, ScheduleObservedAt: map[string]time.Time{testVideoID: observed},
		CollectorInstance: proof.OwnerInstance, Lease: proof,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := repo.PublishBatch(ctx, publishInput(&envelope)); err != nil {
		t.Fatal(err)
	}

	var (
		stored   map[string]time.Time
		evidence string
	)

	if err := pool.QueryRow(ctx, `SELECT schedule_observed_at, encode(evidence_sha256, 'hex') FROM source_observations`).Scan(&stored, &evidence); err != nil {
		t.Fatal(err)
	}

	if evidence != envelope.EvidenceSHA256 {
		t.Fatalf("stored evidence = %s, want %s", evidence, envelope.EvidenceSHA256)
	}

	rebuilt := envelope

	rebuilt.ScheduleObservedAt = stored

	if _, err := rebuilt.ValidateAndCanonicalPayload(); err != nil {
		t.Fatalf("stored schedule observations %v fail evidence validation: %v", stored, err)
	}
}
