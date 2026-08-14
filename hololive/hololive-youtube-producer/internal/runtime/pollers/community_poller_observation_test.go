package pollers

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/community"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
)

func TestCommunityObservationConsumerProcessesImmutableEvidenceQueue(t *testing.T) {
	channelID := "UC_CONSUME"
	db := newCommunityObservationTestDB(t, channelID)
	consumer := NewCommunityObservationConsumer(db.Pool, nil, "test-owner", slog.Default())
	require.NotNil(t, consumer)
	proof := seedCommunityPublishLease(t, db, channelID)
	repo := sourceobservation.NewRepository(db.Pool)

	publishCommunityObservation(t, repo, proof, channelID, "post-1")
	require.NoError(t, consumer.tick(context.Background()))
	require.EqualValues(t, 1, communityTableCount(t, db, "youtube_community_posts"))
	require.EqualValues(t, 1, communityTableCount(t, db, "youtube_notification_outbox"))
	require.EqualValues(t, 1, communityObservationCount(t, db, channelID))
	require.Equal(t, string(contract.StatusProcessed), communityObservationStatus(t, db, channelID))
}

func TestCommunityObservationConsumerSharesNormalizedKeywords(t *testing.T) {
	raw := []string{" HoloLive ", "STREAM", "stream"}
	db := newCommunityObservationTestDB(t, "UC_KEYS")
	consumer := NewCommunityObservationConsumer(db.Pool, raw, "ap-c", slog.Default())
	require.NotNil(t, consumer)
	require.Equal(t, community.NormalizeKeywords(raw), consumer.keywords)
}

func newCommunityObservationTestDB(t *testing.T, channelID string) *pollerBatchTestDB {
	t.Helper()
	db := newPollerBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	require.NoError(t, db.Create(&domain.YouTubeContentWatermark{
		ChannelID:     channelID,
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: "old-post",
	}).Error)
	return db
}

func seedCommunityPublishLease(
	t *testing.T,
	db *pollerBatchTestDB,
	channelID string,
) contract.LeaseProof {
	t.Helper()
	ctx := context.Background()
	var generation int64
	require.NoError(t, db.QueryRow(ctx, `
		INSERT INTO youtube_collection_projection_generations (
			status, row_count, projection_sha256, valid_until, activated_at
		) VALUES ('CURRENT', 1, $1, NOW() + INTERVAL '1 day', NOW())
		RETURNING generation
	`, strings.Repeat("a", 64)).Scan(&generation))
	_, err := db.Exec(ctx, `
		INSERT INTO youtube_collection_targets (
			projection_generation, subject_key, observation_kind,
			priority, poll_interval_ms, enabled, valid_until
		) VALUES ($1, $2, 'community_page', 50, 60000, TRUE, NOW() + INTERVAL '1 day')
	`, generation, channelID)
	require.NoError(t, err)
	proof := contract.LeaseProof{
		JobKey:               "job:community:" + channelID,
		CollectionJobKind:    "community_collect",
		OwnerInstance:        "collector-a",
		FenceEpoch:           1,
		ProjectionGeneration: generation,
		ScheduledFor:         time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC),
	}
	_, err = db.Exec(ctx, `
		INSERT INTO youtube_collection_job_leases (
			job_key, provider, job_class, collection_job_kind, subject_key,
			projection_generation, poll_interval_ms, slot_state, scheduled_for,
			next_due_at, fence_epoch, owner_instance, lease_expires_at
		) VALUES ($1, 'youtubejs', 'SUBJECT', 'community_collect', $2, $3, 60000,
		          'ACTIVE', $4, $4, 1, $5, NOW() + INTERVAL '1 hour')
	`, proof.JobKey, channelID, generation, proof.ScheduledFor, proof.OwnerInstance)
	require.NoError(t, err)
	return proof
}

func publishCommunityObservation(
	t *testing.T,
	repo *sourceobservation.Repository,
	proof contract.LeaseProof,
	channelID string,
	postIDs ...string,
) {
	t.Helper()
	posts := make([]contract.CommunityPostV1, 0, len(postIDs))
	for _, postID := range postIDs {
		posts = append(posts, contract.CommunityPostV1{PostID: postID, ChannelID: channelID})
	}
	payload, err := contract.MarshalPayloadV1(contract.CommunityPayloadV1{
		ChannelID: channelID,
		Posts:     posts,
		Coverage: contract.CommunityPageCoverageV1{
			ChannelID: channelID, MaxResults: 10, PageCount: 1, Exhausted: true,
		},
	})
	require.NoError(t, err)
	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider:           contract.ProviderYouTubeJS,
		ObservationKind:    contract.KindCommunityPage,
		SubjectKey:         channelID,
		SchemaVersion:      1,
		ContractGeneration: 1,
		ScheduledFor:       proof.ScheduledFor,
		ObservedAt:         proof.ScheduledFor.Add(time.Second),
		Completeness:       contract.CompletenessComplete,
		Continuity:         contract.ContinuityContiguous,
		Payload:            payload,
		CollectorInstance:  proof.OwnerInstance,
		Lease:              proof,
	})
	require.NoError(t, err)
	_, err = repo.PublishBatch(context.Background(), sourceobservation.PublishBatchInput{
		Lease: proof,
		Checkpoint: sourceobservation.CheckpointUpdate{
			Entries: []sourceobservation.CheckpointEntry{{
				Provider:           envelope.Provider,
				ObservationKind:    envelope.ObservationKind,
				SubjectKey:         envelope.SubjectKey,
				ScopeSHA256:        envelope.ScopeSHA256,
				ContractGeneration: envelope.ContractGeneration,
				LastObservationKey: envelope.ObservationKey,
				LastEvidenceSHA256: envelope.EvidenceSHA256,
				LastScheduledFor:   envelope.ScheduledFor,
				Continuity:         envelope.Continuity,
			}},
			CollectionLatency: time.Second,
		},
		Observations: []contract.Envelope{envelope},
	})
	require.NoError(t, err)
}

func communityTableCount(t *testing.T, db *pollerBatchTestDB, table string) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&count))
	return count
}

func communityObservationCount(t *testing.T, db *pollerBatchTestDB, subjectKey string) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.QueryRow(context.Background(), `
		SELECT count(*) FROM source_observations
		WHERE provider = 'youtubejs' AND observation_kind = 'community_page' AND subject_key = $1
	`, subjectKey).Scan(&count))
	return count
}

func communityObservationStatus(t *testing.T, db *pollerBatchTestDB, subjectKey string) string {
	t.Helper()
	var status string
	require.NoError(t, db.QueryRow(context.Background(), `
		SELECT queue.status
		FROM source_observation_queue AS queue
		JOIN source_observations AS observation ON observation.id = queue.observation_id
		WHERE observation.subject_key = $1
		ORDER BY observation.id DESC
		LIMIT 1
	`, subjectKey).Scan(&status))
	return status
}
