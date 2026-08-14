package pollers

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/community"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
)

func newSharedCommunityConsumer(t *testing.T, db *pollerBatchTestDB, keywords []string) *sourceobservation.Consumer {
	t.Helper()
	repo := sourceobservation.NewRepository(db.Pool)
	writer := sourceobservation.NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(db.Pool, nil))
	return sourceobservation.NewConsumer(repo, writer, keywords)
}

func consumeCommunity(t *testing.T, consumer *sourceobservation.Consumer) error {
	t.Helper()
	return consumer.Consume(context.Background(), sourceobservation.ClaimOptions{
		ConsumerName:  "hololive-api-youtube",
		LeaseOwner:    "test-owner",
		Kinds:         []contract.ObservationKind{contract.KindCommunityPage},
		Limit:         10,
		LeaseDuration: 30 * time.Second,
	})
}

func TestCommunityObservationConsumerProcessesImmutableEvidenceQueue(t *testing.T) {
	channelID := "UC_CONSUME"
	db := newCommunityObservationTestDB(t, channelID)
	consumer := newSharedCommunityConsumer(t, db, nil)
	proof := seedCommunityPublishLease(t, db, channelID)
	repo := sourceobservation.NewRepository(db.Pool)

	publishCommunityObservation(t, repo, proof, channelID, "post-1")
	require.NoError(t, consumeCommunity(t, consumer))
	require.EqualValues(t, 1, communityTableCount(t, db, "youtube_community_posts"))
	require.EqualValues(t, 1, communityTableCount(t, db, "youtube_notification_outbox"))
	require.EqualValues(t, 1, communityObservationCount(t, db, channelID))
	require.Equal(t, string(contract.StatusProcessed), communityObservationStatus(t, db, channelID))
}

func TestCommunityObservationConsumerSharesNormalizedKeywords(t *testing.T) {
	raw := []string{" HoloLive ", "STREAM", "stream"}
	require.Equal(t, community.NormalizeKeywords(raw), community.NormalizeKeywords([]string{"hololive", "stream"}))
}

func TestCommunityObservationConsumerDoesNotRegressCanonicalStateWhenOlderObservationFinishesLast(t *testing.T) {
	channelID := "UC_OUT_OF_ORDER"
	db := newCommunityObservationTestDB(t, channelID)
	consumer := newSharedCommunityConsumer(t, db, nil)
	repo := sourceobservation.NewRepository(db.Pool)
	oldProof := seedCommunityPublishLease(t, db, channelID)

	publishCommunityObservationWithPosts(t, repo, oldProof, channelID, []contract.CommunityPostV1{
		{PostID: "old-head", ChannelID: channelID},
		{PostID: "shared", ChannelID: channelID, LikeCount: 1, CommentCount: 2},
	})
	newProof := advanceCommunityPublishLease(t, db, oldProof)
	publishCommunityObservationWithPosts(t, repo, newProof, channelID, []contract.CommunityPostV1{
		{PostID: "new-head", ChannelID: channelID},
		{PostID: "shared", ChannelID: channelID, LikeCount: 9, CommentCount: 11},
	})

	_, err := db.Exec(context.Background(), mustTestSQL("defer_oldest_community_observation.sql"), channelID)
	require.NoError(t, err)
	require.NoError(t, consumeCommunity(t, consumer))
	_, err = db.Exec(context.Background(), mustTestSQL("make_oldest_community_observation_due.sql"), channelID)
	require.NoError(t, err)
	require.NoError(t, consumeCommunity(t, consumer))

	var watermark string
	require.NoError(t, db.QueryRow(context.Background(), mustTestSQL("community_watermark.sql"), channelID).Scan(&watermark))
	require.Equal(t, "community:new-head", watermark)
	var likeCount, commentCount int64
	require.NoError(t, db.QueryRow(context.Background(), mustTestSQL("community_post_counts.sql"), "community:shared").Scan(&likeCount, &commentCount))
	require.EqualValues(t, 9, likeCount)
	require.EqualValues(t, 11, commentCount)
	var staleCount int64
	require.NoError(t, db.QueryRow(context.Background(), mustTestSQL("community_stale_application_count.sql"), channelID).Scan(&staleCount))
	require.EqualValues(t, 1, staleCount)

	_, err = db.Exec(context.Background(), mustTestSQL("delete_community_subject_head_observation.sql"), channelID)
	require.NoError(t, err)
	latestProof := advanceCommunityPublishLease(t, db, newProof)
	publishCommunityObservationWithPosts(t, repo, latestProof, channelID, []contract.CommunityPostV1{
		{PostID: "latest-head", ChannelID: channelID},
		{PostID: "shared", ChannelID: channelID, LikeCount: 20, CommentCount: 21},
	})
	require.NoError(t, consumeCommunity(t, consumer))
	require.NoError(t, db.QueryRow(context.Background(), mustTestSQL("community_watermark.sql"), channelID).Scan(&watermark))
	require.Equal(t, "community:latest-head", watermark)
	require.NoError(t, db.QueryRow(context.Background(), mustTestSQL("community_post_counts.sql"), "community:shared").Scan(&likeCount, &commentCount))
	require.EqualValues(t, 20, likeCount)
	require.EqualValues(t, 21, commentCount)
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
	require.NoError(t, db.QueryRow(ctx, mustTestSQL("insert_community_projection.sql"), strings.Repeat("a", 64)).Scan(&generation))
	_, err := db.Exec(ctx, mustTestSQL("insert_community_target.sql"), generation, channelID)
	require.NoError(t, err)
	proof := contract.LeaseProof{
		JobKey:               "job:community:" + channelID,
		CollectionJobKind:    "community_collect",
		OwnerInstance:        "collector-a",
		FenceEpoch:           1,
		ProjectionGeneration: generation,
		ScheduledFor:         time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC),
	}
	_, err = db.Exec(ctx, mustTestSQL("insert_community_job_lease.sql"), proof.JobKey, channelID, generation, proof.ScheduledFor, proof.OwnerInstance)
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
	publishCommunityObservationWithPosts(t, repo, proof, channelID, posts)
}

func publishCommunityObservationWithPosts(
	t *testing.T,
	repo *sourceobservation.Repository,
	proof contract.LeaseProof,
	channelID string,
	posts []contract.CommunityPostV1,
) {
	t.Helper()
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

func advanceCommunityPublishLease(t *testing.T, db *pollerBatchTestDB, proof contract.LeaseProof) contract.LeaseProof {
	t.Helper()
	proof.FenceEpoch++
	proof.ScheduledFor = proof.ScheduledFor.Add(time.Minute)
	_, err := db.Exec(
		context.Background(),
		mustTestSQL("advance_community_job_lease.sql"),
		proof.JobKey,
		proof.OwnerInstance,
		proof.FenceEpoch,
		proof.ScheduledFor,
	)
	require.NoError(t, err)
	return proof
}

func communityTableCount(t *testing.T, db *pollerBatchTestDB, table string) int64 {
	t.Helper()
	var count int64
	queryName := map[string]string{
		"youtube_community_posts":     "community_post_count.sql",
		"youtube_notification_outbox": "community_outbox_count.sql",
	}[table]
	require.NotEmpty(t, queryName)
	require.NoError(t, db.QueryRow(context.Background(), mustTestSQL(queryName)).Scan(&count))
	return count
}

func communityObservationCount(t *testing.T, db *pollerBatchTestDB, subjectKey string) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.QueryRow(context.Background(), mustTestSQL("community_observation_count.sql"), subjectKey).Scan(&count))
	return count
}

func communityObservationStatus(t *testing.T, db *pollerBatchTestDB, subjectKey string) string {
	t.Helper()
	var status string
	require.NoError(t, db.QueryRow(context.Background(), mustTestSQL("latest_community_observation_status.sql"), subjectKey).Scan(&status))
	return status
}
