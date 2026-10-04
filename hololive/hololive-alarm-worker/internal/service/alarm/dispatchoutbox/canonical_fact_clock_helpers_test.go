package dispatchoutbox_test

import (
	"context"
	"encoding/json/jsontext"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	consumekit "github.com/kapu/hololive-api/testkit/sourceobservation"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	publishkit "github.com/kapu/hololive-youtube-collector/testkit/sourceobservation"
)

const (
	clockChannelID     = "UC_TEST"
	clockVideoID       = "vid-a"
	clockAPILeaseOwner = "api-a"
)

func clockSeedPublishLease(
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

	if err := pool.QueryRow(ctx, `
		INSERT INTO youtube_collection_projection_generations (
			status, row_count, projection_sha256, valid_until, activated_at
		) VALUES ('CURRENT', 1, $1, NOW() + INTERVAL '1 day', NOW())
		RETURNING generation
	`, strings.Repeat("a", 64)).Scan(&generation); err != nil {
		tb.Fatalf("seed projection: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_collection_targets (
			projection_generation, subject_key, observation_kind,
			priority, poll_interval_ms, enabled, member_since_generation
		) VALUES ($1, $2, $3, 50, 60000, TRUE, $1)
	`, generation, subjectKey, kind); err != nil {
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

	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_collection_job_leases (
			job_key, provider, job_class, collection_job_kind, subject_key,
			projection_generation, poll_interval_ms, slot_state, scheduled_for,
			next_due_at, fence_epoch, owner_instance, lease_expires_at,
			membership_kinds, membership_exact_subject, membership_target_count
		) VALUES ($1, $2, $3, $4, $5, $6, 60000, 'ACTIVE', $7, $7, $8, $9, NOW() + INTERVAL '1 hour',
		          CASE $4
		              WHEN 'youtubejs_content' THEN ARRAY['shorts_list', 'video_list']
		              WHEN 'youtubejs_channel_metadata' THEN ARRAY['channel_photo', 'channel_profile']
		              WHEN 'holodex_schedule' THEN ARRAY['live_snapshot', 'schedule_snapshot']
		              ELSE ARRAY[$10]::text[]
		          END, $11, 1)
	`, proof.JobKey, provider, jobClass, jobKind, subjectKey, generation,
		proof.ScheduledFor, proof.FenceEpoch, proof.OwnerInstance, kind, !strings.HasPrefix(jobKind, "holodex_")); err != nil {
		tb.Fatalf("seed lease: %v", err)
	}

	return proof
}

func clockAdvanceLease(
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

	if _, err := pool.Exec(ctx, `
		UPDATE youtube_collection_job_leases
		SET slot_state = 'ACTIVE', owner_instance = $2, lease_expires_at = NOW() + INTERVAL '1 hour',
		    retry_not_before = NULL, fence_epoch = $3, scheduled_for = $4, next_due_at = $4
		WHERE job_key = $1
	`, next.JobKey, next.OwnerInstance, next.FenceEpoch, next.ScheduledFor); err != nil {
		tb.Fatalf("advance lease: %v", err)
	}

	return next
}

func clockPublishInput(envelope *contract.Envelope) *publishkit.PublishBatchInput {
	return &publishkit.PublishBatchInput{
		Lease: envelope.Lease,
		Checkpoint: publishkit.CheckpointUpdate{
			Entries: []publishkit.CheckpointEntry{func() publishkit.CheckpointEntry {
				entry := clockCheckpointForEnvelope(envelope)

				entry.Cursor = jsontext.Value(`{"page":1}`)

				return entry
			}()},
			CollectionLatency: time.Second,
		},
		Observations: []contract.Envelope{*envelope},
	}
}

func clockCheckpointForEnvelope(envelope *contract.Envelope) publishkit.CheckpointEntry {
	return publishkit.CheckpointEntry{
		Provider:           envelope.Provider,
		ObservationKind:    envelope.ObservationKind,
		SubjectKey:         envelope.SubjectKey,
		ScopeSHA256:        envelope.ScopeSHA256,
		ContractGeneration: envelope.ContractGeneration,
		LastObservationKey: envelope.ObservationKey,
		LastEvidenceSHA256: envelope.EvidenceSHA256,
		LastScheduledFor:   envelope.ScheduledFor,
		Continuity:         envelope.Continuity,
	}
}

func clockContentClaimOptions() consumekit.ClaimOptions {
	return consumekit.ClaimOptions{
		ConsumerName:  "youtube-content-processor",
		LeaseOwner:    clockAPILeaseOwner,
		Kinds:         []contract.ObservationKind{contract.KindVideoList, contract.KindShortsList},
		Limit:         10,
		LeaseDuration: 30 * time.Second,
	}
}

func clockSeedContentWatermark(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO youtube_content_watermarks (channel_id, watermark_type, initialized, last_content_id)
		VALUES ($1, 'VIDEO', TRUE, 'old-video')
	`, clockChannelID); err != nil {
		t.Fatalf("seed video watermark: %v", err)
	}

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO youtube_content_channel_heads (channel_id, observation_kind, earliest_complete_effective_at)
		VALUES ($1, 'video_list', TIMESTAMPTZ '2026-08-01 00:00:00+00')
	`, clockChannelID); err != nil {
		t.Fatalf("seed content channel head: %v", err)
	}
}

func clockPremiereVideoListEnvelope(t *testing.T, proof *contract.LeaseProof, scheduled time.Time) *contract.Envelope {
	t.Helper()

	checkedAt := proof.ScheduledFor.Add(time.Second)

	payload, err := contract.MarshalPayloadV1(contract.VideoListV1{
		ChannelID: clockChannelID,
		Videos: []contract.VideoListItemV1{{
			VideoID:      clockVideoID,
			ChannelID:    clockChannelID,
			Title:        "Premiere title",
			ScheduledFor: &scheduled,
			IsPremiere:   new(true),
			Publication: &contract.VideoPublicationV1{
				Status:       contract.VideoPublicationUpcomingPremiere,
				ScheduledFor: &scheduled,
				CheckedAt:    checkedAt,
			},
		}},
		Coverage: contract.ChannelListCoverageV1{
			ChannelID:  clockChannelID,
			MaxResults: 10,
			Exhausted:  true,
		},
	})
	if err != nil {
		t.Fatalf("marshal Premiere video list payload: %v", err)
	}

	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider:           contract.ProviderYouTubeJS,
		ObservationKind:    contract.KindVideoList,
		SubjectKey:         clockChannelID,
		SchemaVersion:      contract.SchemaVersionV1,
		ContractGeneration: contract.VideoListPublicationContractGeneration,
		ScheduledFor:       proof.ScheduledFor,
		ObservedAt:         checkedAt,
		Completeness:       contract.CompletenessComplete,
		Continuity:         contract.ContinuityContiguous,
		Payload:            payload,
		CollectorInstance:  proof.OwnerInstance,
		Lease:              *proof,
	})
	if err != nil {
		t.Fatalf("prepare Premiere video list envelope: %v", err)
	}

	return &envelope
}

func clockScheduleEnvelope(t *testing.T, proof *contract.LeaseProof, items ...contract.ScheduleItemV1) *contract.Envelope {
	t.Helper()

	payload, err := contract.MarshalPayloadV1(contract.ScheduleSnapshotV1{
		GroupKey: "global:hololive-schedule",
		Items:    items,
		Coverage: contract.ScheduleCoverageV1{GroupKey: "global:hololive-schedule"},
	})
	if err != nil {
		t.Fatalf("marshal schedule: %v", err)
	}

	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider: clockProofProvider(proof), ObservationKind: contract.KindSchedule, SubjectKey: "global:hololive-schedule",
		SchemaVersion: contract.SchemaVersionV1, ContractGeneration: 1,
		ScheduledFor: proof.ScheduledFor, ObservedAt: proof.ScheduledFor.Add(time.Second),
		Completeness: contract.CompletenessComplete, Continuity: contract.ContinuityNotApplicable,
		Payload: payload, CollectorInstance: proof.OwnerInstance, Lease: *proof,
	})
	if err != nil {
		t.Fatalf("prepare schedule: %v", err)
	}

	return &envelope
}

func clockProofProvider(proof *contract.LeaseProof) contract.Provider {
	if proof.CollectionJobKind == "holodex_schedule" {
		return contract.ProviderHolodex
	}

	return contract.ProviderHololiveOfficial
}

func clockLiveClaimOptions() consumekit.ClaimOptions {
	return consumekit.ClaimOptions{
		ConsumerName:  "youtube-live-processor",
		LeaseOwner:    clockAPILeaseOwner,
		Kinds:         []contract.ObservationKind{contract.KindLiveSnapshot, contract.KindViewerSample, contract.KindSchedule},
		Limit:         10,
		LeaseDuration: 30 * time.Second,
	}
}
