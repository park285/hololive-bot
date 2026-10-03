package canonicalwrite

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestPersistCommunityPostsTxConflictWithSentOutboxBackfillsTrackingSentState(t *testing.T) {
	pool := dbtest.NewPool(t)
	publishedAt := time.Date(2026, time.April, 10, 1, 11, 12, 0, time.UTC)
	detectedAt := publishedAt.Add(20 * time.Second)
	sentAt := detectedAt.Add(40 * time.Second)
	createdAt := publishedAt.Add(-5 * time.Minute)

	seedOutbox(t, pool, &domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindCommunityPost,
		ChannelID:     testChannelID,
		ContentID:     testPostID,
		Payload:       `{"canonical_post_id":"community:post-1","post_id":"post-1"}`,
		Status:        domain.OutboxStatusSent,
		AttemptCount:  1,
		NextAttemptAt: createdAt,
		CreatedAt:     createdAt,
		SentAt:        &sentAt,
	})

	persistCommunityPostWithTracking(t, pool, publishedAt, detectedAt)

	trackingRow := getRow[domain.YouTubeContentAlarmTracking](t, pool,
		`SELECT `+trackingColumns+` FROM youtube_content_alarm_tracking WHERE kind = $1 AND content_id = $2`, domain.OutboxKindCommunityPost, testPostID)
	require.NotNil(t, trackingRow.AlarmSentAt)
	require.Equal(t, sentAt, trackingRow.AlarmSentAt.UTC())
	require.Equal(t, domain.YouTubeContentAlarmDeliveryStatusSent, trackingRow.DeliveryStatus)
}

func TestPersistCommunityPostsTxConflictWithSentDeliveryBackfillsTrackingSentState(t *testing.T) {
	pool := dbtest.NewPool(t)
	publishedAt := time.Date(2026, time.April, 10, 1, 11, 12, 0, time.UTC)
	detectedAt := publishedAt.Add(20 * time.Second)
	sentAt := detectedAt.Add(40 * time.Second)
	createdAt := publishedAt.Add(-5 * time.Minute)
	nextAttemptAt := publishedAt.Add(-1 * time.Minute)

	outboxID := seedOutbox(t, pool, &domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindCommunityPost,
		ChannelID:     testChannelID,
		ContentID:     testPostID,
		Payload:       `{"canonical_post_id":"community:post-1","post_id":"post-1"}`,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  1,
		NextAttemptAt: nextAttemptAt,
		CreatedAt:     createdAt,
	})
	seedDelivery(t, pool, &domain.YouTubeNotificationDelivery{
		OutboxID:      outboxID,
		RoomID:        "room-1",
		Status:        domain.OutboxStatusSent,
		AttemptCount:  1,
		NextAttemptAt: nextAttemptAt,
		CreatedAt:     createdAt,
		SentAt:        &sentAt,
	})

	persistCommunityPostWithTracking(t, pool, publishedAt, detectedAt)

	trackingRow := getRow[domain.YouTubeContentAlarmTracking](t, pool,
		`SELECT `+trackingColumns+` FROM youtube_content_alarm_tracking WHERE kind = $1 AND content_id = $2`, domain.OutboxKindCommunityPost, testPostID)
	require.NotNil(t, trackingRow.AlarmSentAt)
	require.Equal(t, sentAt, trackingRow.AlarmSentAt.UTC())
	require.Equal(t, domain.YouTubeContentAlarmDeliveryStatusSent, trackingRow.DeliveryStatus)
}

func persistCommunityPostWithTracking(t *testing.T, pool *pgxpool.Pool, publishedAt, detectedAt time.Time) {
	t.Helper()

	post := communityTestPost(testPostID, &publishedAt, 10, 2)

	require.NoError(t, commitCommunityPosts(t.Context(), pool, []*domain.YouTubeCommunityPost{post}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindCommunityPost,
		ChannelID: testChannelID,
		ContentID: testPostID,
		Payload:   communityPayload(t, post, testPostID, testCanonicalPostID),
		Status:    domain.OutboxStatusPending,
	}}, []*domain.YouTubeContentAlarmTracking{{
		Kind:               domain.OutboxKindCommunityPost,
		ContentID:          testPostID,
		CanonicalContentID: testCanonicalPostID,
		ChannelID:          testChannelID,
		ActualPublishedAt:  &publishedAt,
		DetectedAt:         detectedAt,
	}}, nil))
}

func TestPersistVideosTxConflictWithSentOutboxBackfillsTrackingSentState(t *testing.T) {
	pool := dbtest.NewPool(t)
	publishedAt := time.Date(2026, time.April, 10, 1, 11, 12, 0, time.UTC)
	detectedAt := publishedAt.Add(20 * time.Second)
	sentAt := detectedAt.Add(40 * time.Second)
	createdAt := publishedAt.Add(-5 * time.Minute)
	shortVideo := &domain.YouTubeVideo{
		VideoID:     testVideoID,
		ChannelID:   testChannelID,
		Title:       testShortTitle,
		IsShort:     true,
		PublishedAt: &publishedAt,
		ViewCount:   42,
	}

	seedOutbox(t, pool, &domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewShort,
		ChannelID:     testChannelID,
		ContentID:     testCanonicalShortFromVideoID,
		Payload:       shortPayload(t, shortVideo, testCanonicalShortFromVideoID),
		Status:        domain.OutboxStatusSent,
		AttemptCount:  1,
		NextAttemptAt: createdAt,
		CreatedAt:     createdAt,
		SentAt:        &sentAt,
	})

	require.NoError(t, commitVideos(t.Context(), pool,
		[]*domain.YouTubeVideo{shortVideo},
		[]*domain.YouTubeNotificationOutbox{{
			Kind:      domain.OutboxKindNewShort,
			ChannelID: testChannelID,
			ContentID: testCanonicalShortFromVideoID,
			Payload:   shortPayload(t, shortVideo, testCanonicalShortFromVideoID),
			Status:    domain.OutboxStatusPending,
		}},
		[]*domain.YouTubeContentAlarmTracking{{
			Kind:              domain.OutboxKindNewShort,
			ContentID:         testCanonicalShortFromVideoID,
			ChannelID:         testChannelID,
			ActualPublishedAt: &publishedAt,
			DetectedAt:        detectedAt,
		}},
		nil,
	))

	trackingRow := getRow[domain.YouTubeContentAlarmTracking](t, pool,
		`SELECT `+trackingColumns+` FROM youtube_content_alarm_tracking WHERE kind = $1 AND content_id = $2`, domain.OutboxKindNewShort, testCanonicalShortFromVideoID)
	require.Equal(t, testCanonicalShortFromVideoID, trackingRow.CanonicalContentID)
	require.NotNil(t, trackingRow.AlarmSentAt)
	require.Equal(t, sentAt, trackingRow.AlarmSentAt.UTC())
	require.Equal(t, domain.YouTubeContentAlarmDeliveryStatusSent, trackingRow.DeliveryStatus)
}

func TestPersistVideosTxDoesNotReactivateFailedOutbox(t *testing.T) {
	pool := dbtest.NewPool(t)
	createdAt := time.Date(2026, time.April, 10, 1, 0, 0, 0, time.UTC)
	nextAttemptAt := createdAt.Add(5 * time.Minute)
	existingID := seedOutbox(t, pool, &domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewVideo,
		ChannelID:     testChannelID,
		ContentID:     testVideoID,
		Payload:       `{"video_id":"video-1","version":"old"}`,
		Status:        domain.OutboxStatusFailed,
		AttemptCount:  3,
		NextAttemptAt: nextAttemptAt,
		CreatedAt:     createdAt,
		Error:         "video failed",
	})

	require.NoError(t, commitVideos(t.Context(), pool, []*domain.YouTubeVideo{{
		VideoID:   testVideoID,
		ChannelID: testChannelID,
		Title:     "title-video-1",
		ViewCount: 999,
	}}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindNewVideo,
		ChannelID: testChannelID,
		ContentID: testVideoID,
		Payload:   `{"video_id":"video-1","version":"new"}`,
		Status:    domain.OutboxStatusPending,
	}}, nil, &domain.YouTubeContentWatermark{
		ChannelID:     testChannelID,
		WatermarkType: domain.WatermarkTypeVideo,
		Initialized:   true,
		LastContentID: testVideoID,
	}))

	outboxRows := selectRows[domain.YouTubeNotificationOutbox](t, pool, `SELECT `+outboxColumns+` FROM youtube_notification_outbox ORDER BY id ASC`)
	require.Len(t, outboxRows, 1)
	require.Equal(t, existingID, outboxRows[0].ID)
	require.Equal(t, domain.OutboxStatusFailed, outboxRows[0].Status)
	require.Equal(t, 3, outboxRows[0].AttemptCount)
	require.Equal(t, nextAttemptAt, outboxRows[0].NextAttemptAt.UTC())
	require.Equal(t, "video failed", outboxRows[0].Error)
	require.Contains(t, outboxRows[0].Payload, `"version": "old"`)
}

// 빈 content_id는 영상 upsert 뒤 outbox 삽입에서 실패하므로, 호출자 트랜잭션 롤백으로만 무기록이 보장된다.
func TestPersistVideosTxRejectsBlankNotificationContentID(t *testing.T) {
	pool := dbtest.NewPool(t)

	err := commitVideos(t.Context(), pool, []*domain.YouTubeVideo{{
		VideoID:   testVideoID,
		ChannelID: testChannelID,
		Title:     "title",
		ViewCount: 1,
	}}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindNewShort,
		ChannelID: testChannelID,
		ContentID: "   ",
		Payload:   `{"video_id":"video-1"}`,
		Status:    domain.OutboxStatusPending,
	}}, nil, &domain.YouTubeContentWatermark{
		ChannelID:     testChannelID,
		WatermarkType: domain.WatermarkTypeShort,
		Initialized:   true,
		LastContentID: testVideoID,
	})
	require.ErrorContains(t, err, "dedupe key")
	require.Zero(t, countRows(t, pool, `SELECT COUNT(*) FROM youtube_videos`))
	require.Zero(t, countRows(t, pool, `SELECT COUNT(*) FROM youtube_notification_outbox`))
}

type duplicatePollClaimCase struct {
	name      string
	kind      domain.OutboxKind
	postID    string
	contentID string
	seed      func(t *testing.T, pool *pgxpool.Pool, publishedAt, detectedAt, authorizedAt time.Time)
	persist   func(t *testing.T, pool *pgxpool.Pool, publishedAt, detectedAt time.Time) error
}

func duplicatePollCommunityPostCase() duplicatePollClaimCase {
	const canonicalPostID = "community:post-duplicate"

	return duplicatePollClaimCase{
		name:      "community post",
		kind:      domain.OutboxKindCommunityPost,
		postID:    canonicalPostID,
		contentID: testDuplicatePostID,
		seed: func(t *testing.T, pool *pgxpool.Pool, publishedAt, detectedAt, authorizedAt time.Time) {
			t.Helper()

			post := communityTestPost(testDuplicatePostID, &publishedAt, 10, 2)
			seedOutbox(t, pool, &domain.YouTubeNotificationOutbox{
				Kind:          domain.OutboxKindCommunityPost,
				ChannelID:     testChannelID,
				ContentID:     testDuplicatePostID,
				Payload:       communityPayload(t, post, testDuplicatePostID, canonicalPostID),
				Status:        domain.OutboxStatusPending,
				NextAttemptAt: authorizedAt,
				CreatedAt:     authorizedAt,
			})
			seedAlarmState(t, pool, &domain.YouTubeCommunityShortsAlarmState{
				Kind:              domain.OutboxKindCommunityPost,
				PostID:            canonicalPostID,
				ContentID:         testDuplicatePostID,
				ChannelID:         testChannelID,
				ActualPublishedAt: &publishedAt,
				DetectedAt:        detectedAt,
				AuthorizedAt:      &authorizedAt,
				DeliveryStatus:    domain.YouTubeCommunityShortsAlarmStateStatusEnqueued,
			})
		},
		persist: func(t *testing.T, pool *pgxpool.Pool, publishedAt, detectedAt time.Time) error {
			t.Helper()

			post := communityTestPost(testDuplicatePostID, &publishedAt, 10, 2)

			return commitCommunityPosts(t.Context(), pool, []*domain.YouTubeCommunityPost{post}, []*domain.YouTubeNotificationOutbox{{
				Kind:      domain.OutboxKindCommunityPost,
				ChannelID: testChannelID,
				ContentID: testDuplicatePostID,
				Payload:   communityPayload(t, post, testDuplicatePostID, canonicalPostID),
				Status:    domain.OutboxStatusPending,
			}}, []*domain.YouTubeContentAlarmTracking{{
				Kind:              domain.OutboxKindCommunityPost,
				ContentID:         testDuplicatePostID,
				ChannelID:         testChannelID,
				ActualPublishedAt: &publishedAt,
				DetectedAt:        detectedAt.Add(time.Minute),
			}}, nil)
		},
	}
}

func duplicatePollShortVideo(publishedAt time.Time) *domain.YouTubeVideo {
	return &domain.YouTubeVideo{
		VideoID:     testDuplicateVideoID,
		ChannelID:   testChannelID,
		Title:       "title-short-duplicate",
		IsShort:     true,
		PublishedAt: &publishedAt,
		ViewCount:   42,
	}
}

func duplicatePollShortCase() duplicatePollClaimCase {
	return duplicatePollClaimCase{
		name:      "short",
		kind:      domain.OutboxKindNewShort,
		postID:    testDuplicateShortContentID,
		contentID: testDuplicateShortContentID,
		seed: func(t *testing.T, pool *pgxpool.Pool, publishedAt, detectedAt, authorizedAt time.Time) {
			t.Helper()

			// 운영의 NEW_SHORT 행은 모두 canonical content_id다(T18 2026-09-26 raw 형식 0건).
			seedOutbox(t, pool, &domain.YouTubeNotificationOutbox{
				Kind:          domain.OutboxKindNewShort,
				ChannelID:     testChannelID,
				ContentID:     testDuplicateShortContentID,
				Payload:       shortPayload(t, duplicatePollShortVideo(publishedAt), testDuplicateShortContentID),
				Status:        domain.OutboxStatusPending,
				NextAttemptAt: authorizedAt,
				CreatedAt:     authorizedAt,
			})
			seedAlarmState(t, pool, &domain.YouTubeCommunityShortsAlarmState{
				Kind:              domain.OutboxKindNewShort,
				PostID:            testDuplicateShortContentID,
				ContentID:         testDuplicateShortContentID,
				ChannelID:         testChannelID,
				ActualPublishedAt: &publishedAt,
				DetectedAt:        detectedAt,
				AuthorizedAt:      &authorizedAt,
				DeliveryStatus:    domain.YouTubeCommunityShortsAlarmStateStatusEnqueued,
			})
		},
		persist: func(t *testing.T, pool *pgxpool.Pool, publishedAt, detectedAt time.Time) error {
			t.Helper()

			video := duplicatePollShortVideo(publishedAt)

			return commitVideos(t.Context(), pool, []*domain.YouTubeVideo{video}, []*domain.YouTubeNotificationOutbox{{
				Kind:      domain.OutboxKindNewShort,
				ChannelID: testChannelID,
				ContentID: testDuplicateShortContentID,
				Payload:   shortPayload(t, video, testDuplicateShortContentID),
				Status:    domain.OutboxStatusPending,
			}}, []*domain.YouTubeContentAlarmTracking{{
				Kind:              domain.OutboxKindNewShort,
				ContentID:         testDuplicateShortContentID,
				ChannelID:         testChannelID,
				ActualPublishedAt: &publishedAt,
				DetectedAt:        detectedAt.Add(time.Minute),
			}}, nil)
		},
	}
}

func TestPersistCommunityShortsDuplicatePollKeepsExistingClaimState(t *testing.T) {
	for _, tc := range []duplicatePollClaimCase{duplicatePollCommunityPostCase(), duplicatePollShortCase()} {
		t.Run(tc.name, func(t *testing.T) {
			pool := dbtest.NewPool(t)
			publishedAt := time.Date(2026, time.April, 10, 1, 11, 12, 0, time.UTC)
			detectedAt := publishedAt.Add(20 * time.Second)
			authorizedAt := detectedAt.Add(30 * time.Second)

			tc.seed(t, pool, publishedAt, detectedAt, authorizedAt)
			require.NoError(t, tc.persist(t, pool, publishedAt, detectedAt))

			require.EqualValues(t, 1, countRows(t, pool,
				`SELECT COUNT(*) FROM youtube_notification_outbox WHERE kind = $1 AND content_id = $2`, tc.kind, tc.contentID))

			trackingRow := getRow[domain.YouTubeContentAlarmTracking](t, pool,
				`SELECT `+trackingColumns+` FROM youtube_content_alarm_tracking WHERE kind = $1 AND content_id = $2`, tc.kind, tc.contentID)
			require.Equal(t, tc.postID, trackingRow.CanonicalContentID)

			stateRows := selectRows[domain.YouTubeCommunityShortsAlarmState](t, pool,
				`SELECT `+alarmStateColumns+` FROM youtube_community_shorts_alarm_states WHERE kind = $1`, tc.kind)
			require.Len(t, stateRows, 1)
			require.Equal(t, tc.postID, stateRows[0].PostID)
			require.Equal(t, tc.contentID, stateRows[0].ContentID)
			require.NotNil(t, stateRows[0].AuthorizedAt)
			require.Equal(t, authorizedAt, stateRows[0].AuthorizedAt.UTC())
			require.Nil(t, stateRows[0].AlarmSentAt)
			require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusEnqueued, stateRows[0].DeliveryStatus)
		})
	}
}
