// Package observationtest는 수집 관측의 발행(sourceobservation)과 소비(consume) 테스트가 함께 쓰는 DB fixture다.
// 수집 job lease를 시드하고 계약 envelope를 만들며 테이블 상태를 확인한다. 테스트 전용이며 운영 코드는 import하지 않는다.
package observationtest

import (
	"context"
	jsonv2 "encoding/json/v2"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

// testChannelID는 두 테스트 패키지의 testChannelID와 같은 채널이다.
const testChannelID = "UC_TEST"

func SeedPublishLease(
	ctx context.Context,
	tb testing.TB,
	pool *pgxpool.Pool,
	provider contract.Provider,
	kind contract.ObservationKind,
	subjectKey string,
	jobKind string,
) contract.LeaseProof {
	tb.Helper()

	scheduledFor := time.Date(2026, time.August, 14, 1, 0, 0, 0, time.UTC)

	var generation int64

	if err := pool.QueryRow(ctx, mustTestSQL("insert_projection_generation.sql"), strings.Repeat("a", 64)).Scan(&generation); err != nil {
		tb.Fatalf("seed projection: %v", err)
	}

	if _, err := pool.Exec(ctx, mustTestSQL("insert_target.sql"), generation, subjectKey, kind); err != nil {
		tb.Fatalf("seed target: %v", err)
	}

	proof := contract.LeaseProof{
		JobKey:               "job:" + jobKind + ":" + subjectKey,
		CollectionJobKind:    jobKind,
		OwnerInstance:        "collector-a",
		FenceEpoch:           1,
		ProjectionGeneration: generation,
		ScheduledFor:         scheduledFor,
	}
	jobClass := "SUBJECT"

	if strings.HasPrefix(jobKind, "holodex_") || jobKind == "official_schedule" {
		jobClass = "GLOBAL"
	}

	if _, err := pool.Exec(ctx, mustTestSQL("insert_job_lease.sql"), proof.JobKey, provider, jobClass, jobKind, subjectKey, generation,
		proof.ScheduledFor, proof.FenceEpoch, proof.OwnerInstance); err != nil {
		tb.Fatalf("seed lease: %v", err)
	}

	return proof
}

func ReactivateLease(ctx context.Context, t *testing.T, pool *pgxpool.Pool, proof *contract.LeaseProof) {
	t.Helper()

	if _, err := pool.Exec(ctx, mustTestSQL("reactivate_job_lease.sql"), proof.JobKey, proof.OwnerInstance); err != nil {
		t.Fatalf("reactivate lease: %v", err)
	}
}

func AdvanceLease(
	ctx context.Context,
	tb testing.TB,
	pool *pgxpool.Pool,
	proof *contract.LeaseProof,
	delta time.Duration,
) contract.LeaseProof {
	tb.Helper()

	next := *proof
	next.FenceEpoch++

	next.ScheduledFor = next.ScheduledFor.Add(delta)

	if _, err := pool.Exec(ctx, mustTestSQL("advance_job_lease.sql"), next.JobKey, next.OwnerInstance, next.FenceEpoch, next.ScheduledFor); err != nil {
		tb.Fatalf("advance lease: %v", err)
	}

	return next
}

func CommunityEnvelope(
	tb testing.TB,
	proof *contract.LeaseProof,
	postID string,
) *contract.Envelope {
	tb.Helper()

	payload, err := contract.MarshalPayloadV1(contract.CommunityPayloadV1{
		ChannelID: testChannelID,
		Posts:     []contract.CommunityPostV1{{PostID: postID, ChannelID: testChannelID}},
		Coverage: contract.CommunityPageCoverageV1{
			ChannelID: testChannelID, MaxResults: 10, PageCount: 1, Exhausted: true,
		},
	})
	if err != nil {
		tb.Fatalf("marshal community payload: %v", err)
	}

	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider:           contract.ProviderYouTubeJS,
		ObservationKind:    contract.KindCommunityPage,
		SubjectKey:         testChannelID,
		SchemaVersion:      contract.SchemaVersionV1,
		ContractGeneration: 1,
		ScheduledFor:       proof.ScheduledFor,
		ObservedAt:         proof.ScheduledFor.Add(time.Second),
		Completeness:       contract.CompletenessComplete,
		Continuity:         contract.ContinuityContiguous,
		Payload:            payload,
		CollectorInstance:  proof.OwnerInstance,
		Lease:              *proof,
	})
	if err != nil {
		tb.Fatalf("prepare community envelope: %v", err)
	}

	return &envelope
}

func ViewerEnvelope(
	t *testing.T,
	proof *contract.LeaseProof,
	generation int64,
	count int64,
) *contract.Envelope {
	t.Helper()

	return ViewerEnvelopeFor(t, proof, generation, "video-1", count)
}

func ViewerEnvelopeFor(
	t *testing.T,
	proof *contract.LeaseProof,
	generation int64,
	subject string,
	count int64,
) *contract.Envelope {
	t.Helper()

	payload, err := contract.MarshalPayloadV1(contract.ViewerSampleV1{
		VideoID:             subject,
		ViewerCount:         &count,
		Availability:        "AVAILABLE",
		SampleWindowStart:   proof.ScheduledFor,
		SampleWindowSeconds: 60,
		Coverage: contract.ViewerSampleCoverageV1{
			VideoID: subject, SampleWindowStart: proof.ScheduledFor, SampleWindowSeconds: 60,
		},
	})
	if err != nil {
		t.Fatalf("marshal viewer payload: %v", err)
	}

	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider:           contract.ProviderHolodex,
		ObservationKind:    contract.KindViewerSample,
		SubjectKey:         subject,
		SchemaVersion:      contract.SchemaVersionV1,
		ContractGeneration: generation,
		ScheduledFor:       proof.ScheduledFor,
		ObservedAt:         proof.ScheduledFor.Add(time.Second),
		Completeness:       contract.CompletenessComplete,
		Continuity:         contract.ContinuityNotApplicable,
		Payload:            payload,
		CollectorInstance:  proof.OwnerInstance,
		Lease:              *proof,
	})
	if err != nil {
		t.Fatalf("prepare viewer envelope: %v", err)
	}

	return &envelope
}

func ChannelPhotoEnvelopeFor(
	t *testing.T,
	proof *contract.LeaseProof,
	subject string,
) *contract.Envelope {
	t.Helper()

	payload, err := contract.MarshalPayloadV1(contract.ChannelPhotoV1{
		ChannelID: subject,
		Variants:  []contract.PhotoVariantV1{{Kind: "avatar", URL: "https://img.test/avatar.jpg"}},
		Coverage:  contract.ChannelPhotoCoverageV1{ChannelID: subject, Variants: []string{"avatar"}},
	})
	if err != nil {
		t.Fatalf("marshal channel photo payload: %v", err)
	}

	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider: contract.ProviderYouTubeJS, ObservationKind: contract.KindChannelPhoto,
		SubjectKey: subject, SchemaVersion: contract.SchemaVersionV1, ContractGeneration: 1,
		ScheduledFor: proof.ScheduledFor, ObservedAt: proof.ScheduledFor.Add(time.Second),
		Completeness: contract.CompletenessComplete, Continuity: contract.ContinuityContiguous,
		Payload: payload, CollectorInstance: proof.OwnerInstance, Lease: *proof,
	})
	if err != nil {
		t.Fatalf("prepare channel photo envelope: %v", err)
	}

	return &envelope
}

func ChannelProfileEnvelope(
	t *testing.T,
	proof *contract.LeaseProof,
	generation int64,
) *contract.Envelope {
	t.Helper()

	return ChannelProfileEnvelopeFor(t, proof, generation, testChannelID)
}

func ChannelProfileEnvelopeFor(
	t *testing.T,
	proof *contract.LeaseProof,
	generation int64,
	subject string,
) *contract.Envelope {
	t.Helper()

	payload, err := contract.MarshalPayloadV1(contract.ChannelProfileV1{
		ChannelID: subject,
		Handle:    contract.FieldValue[string]{Present: true, Value: "test"},
		Coverage: contract.ChannelProfileCoverageV1{
			ChannelID: subject, Fields: []string{"handle"},
		},
	})
	if err != nil {
		t.Fatalf("marshal channel profile payload: %v", err)
	}

	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider:           contract.ProviderYouTubeJS,
		ObservationKind:    contract.KindChannelProfile,
		SubjectKey:         subject,
		SchemaVersion:      contract.SchemaVersionV1,
		ContractGeneration: generation,
		ScheduledFor:       proof.ScheduledFor,
		ObservedAt:         proof.ScheduledFor.Add(time.Second),
		Completeness:       contract.CompletenessComplete,
		Continuity:         contract.ContinuityContiguous,
		Payload:            payload,
		CollectorInstance:  proof.OwnerInstance,
		Lease:              *proof,
	})
	if err != nil {
		t.Fatalf("prepare channel profile envelope: %v", err)
	}

	return &envelope
}

func SeedAdditionalLease(
	t *testing.T,
	pool *pgxpool.Pool,
	existing *contract.LeaseProof,
	provider contract.Provider,
	kind contract.ObservationKind,
	subjectKey string,
	jobKind string,
) contract.LeaseProof {
	t.Helper()

	proof := *existing

	proof.JobKey = "job:" + jobKind + ":" + subjectKey
	proof.CollectionJobKind = jobKind

	if _, err := pool.Exec(t.Context(), mustTestSQL("insert_target_if_absent.sql"), proof.ProjectionGeneration, subjectKey, kind); err != nil {
		t.Fatalf("seed additional target: %v", err)
	}

	jobClass := "SUBJECT"

	if jobKind == "holodex_metadata" || jobKind == "official_schedule" {
		jobClass = "GLOBAL"
	}

	if _, err := pool.Exec(t.Context(), mustTestSQL("insert_job_lease.sql"), proof.JobKey, provider, jobClass, jobKind, subjectKey, proof.ProjectionGeneration,
		proof.ScheduledFor, proof.FenceEpoch, proof.OwnerInstance); err != nil {
		t.Fatalf("seed additional lease: %v", err)
	}

	return proof
}

func IndependentCommunityEnvelope(t *testing.T, proof *contract.LeaseProof) *contract.Envelope {
	t.Helper()

	independent := CommunityEnvelope(t, proof, "post-independent")

	var independentPayload contract.CommunityPayloadV1

	if err := jsonv2.Unmarshal(independent.Payload, &independentPayload); err != nil {
		t.Fatalf("decode independent payload: %v", err)
	}

	independentPayload.Coverage.MaxResults = 20

	encodedPayload, err := contract.MarshalPayloadV1(independentPayload)
	if err != nil {
		t.Fatalf("marshal independent payload: %v", err)
	}

	independent.Payload = encodedPayload

	preparedIndependent, err := contract.PrepareEnvelope(*independent)
	if err != nil {
		t.Fatalf("prepare independent envelope: %v", err)
	}

	independent = &preparedIndependent

	return independent
}

func AssertMixedPersistence(ctx context.Context, t *testing.T, pool *pgxpool.Pool, baseID int64, independent *contract.Envelope) {
	t.Helper()

	var observations, queue, checkpoints, collisions int

	if err := pool.QueryRow(ctx, mustTestSQL("select_mixed_persistence_counts.sql")).Scan(&observations, &queue, &checkpoints, &collisions); err != nil {
		t.Fatalf("count mixed persistence: %v", err)
	}

	if observations != 2 || queue != 2 || checkpoints != 2 || collisions != 1 {
		t.Fatalf("mixed persistence counts = observations %d, queue %d, checkpoints %d, collisions %d; want 2, 2, 2, 1",
			observations, queue, checkpoints, collisions)
	}

	AssertMixedCheckpoint(ctx, t, pool, independent)
	AssertMixedQueue(ctx, t, pool, baseID, independent)
}

func AssertMixedCheckpoint(ctx context.Context, t *testing.T, pool *pgxpool.Pool, independent *contract.Envelope) {
	t.Helper()

	var checkpointObservationKey string

	if err := pool.QueryRow(ctx, mustTestSQL("select_checkpoint_last_key.sql"), independent.Provider, independent.ObservationKind, independent.SubjectKey, independent.ScopeSHA256).Scan(&checkpointObservationKey); err != nil {
		t.Fatalf("load independent checkpoint: %v", err)
	}

	if checkpointObservationKey != independent.ObservationKey {
		t.Fatalf("independent checkpoint key = %s, want %s", checkpointObservationKey, independent.ObservationKey)
	}
}

func AssertMixedQueue(ctx context.Context, t *testing.T, pool *pgxpool.Pool, baseID int64, independent *contract.Envelope) {
	t.Helper()

	var (
		count           int
		firstID, lastID int64
	)

	if err := pool.QueryRow(ctx, mustTestSQL("select_queue_range.sql")).Scan(&count, &firstID, &lastID); err != nil {
		t.Fatalf("load mixed queue: %v", err)
	}

	var independentID int64

	if err := pool.QueryRow(ctx, mustTestSQL("select_observation_id_by_key.sql"), independent.ObservationKey).Scan(&independentID); err != nil {
		t.Fatalf("load independent observation: %v", err)
	}

	if count != 2 || firstID != baseID || lastID != independentID {
		t.Fatalf("mixed queue IDs = count:%d first:%d last:%d, want count:2 first:%d last:%d", count, firstID, lastID, baseID, independentID)
	}
}
