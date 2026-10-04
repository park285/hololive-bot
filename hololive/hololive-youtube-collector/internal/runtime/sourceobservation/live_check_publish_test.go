package sourceobservation

import (
	"errors"
	"testing"
	"time"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
)

// 방송 탭 snapshot job은 채널 확인을 발행할 수 없다. 공유-job collector의 확인이 새 슬롯 계약을 우회하지 못한다.
func TestChannelLiveCheckRejectsSnapshotJobLease(t *testing.T) {
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := seedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindLiveSnapshot, testChannelID, "youtubejs_channel_live")
	ctx := t.Context()

	seedAdditionalLease(t, pool, &proof, contract.ProviderYouTubeJS, contract.KindChannelLiveCheck, testChannelID, "youtubejs_channel_live_check")

	_, err := repo.PublishBatch(ctx, publishInput(channelLiveCheckEnvelope(t, &proof, contract.ChannelLiveCheckV1{
		Outcome: contract.ChannelLiveCheckChannelPage, ChannelIdentityConfirmed: true,
	})))
	if !errors.Is(err, collection.ErrTargetDisabled) {
		t.Fatalf("channel live check under snapshot job lease: err = %v, want %v", err, collection.ErrTargetDisabled)
	}

	assertTableCount(t, pool, "source_observations", 0)
}

func channelLiveCheckEnvelope(t *testing.T, proof *contract.LeaseProof, payload contract.ChannelLiveCheckV1) *contract.Envelope {
	t.Helper()

	payload.ChannelID = testChannelID
	payload.Coverage = contract.ChannelLiveCheckCoverageV1{ChannelID: testChannelID}

	completeness := contract.CompletenessComplete

	if payload.Outcome == contract.ChannelLiveCheckUnknown {
		completeness = contract.CompletenessUnknown
	}

	return liveCheckEnvelope(t, proof, contract.KindChannelLiveCheck, testChannelID, completeness, payload)
}

func liveCheckEnvelope(
	t *testing.T,
	proof *contract.LeaseProof,
	kind contract.ObservationKind,
	subjectKey string,
	completeness contract.Completeness,
	payload any,
) *contract.Envelope {
	t.Helper()

	raw, err := contract.MarshalPayloadV1(payload)
	if err != nil {
		t.Fatalf("marshal %s payload: %v", kind, err)
	}

	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider: contract.ProviderYouTubeJS, ObservationKind: kind, SubjectKey: subjectKey,
		SchemaVersion: contract.SchemaVersionV1, ContractGeneration: contract.LiveCheckContractGeneration,
		ScheduledFor: proof.ScheduledFor, ObservedAt: proof.ScheduledFor.Add(time.Second),
		Completeness: completeness, Continuity: contract.ContinuityNotApplicable,
		Payload: raw, CollectorInstance: proof.OwnerInstance, Lease: *proof,
	})
	if err != nil {
		t.Fatalf("prepare %s envelope: %v", kind, err)
	}

	return &envelope
}
