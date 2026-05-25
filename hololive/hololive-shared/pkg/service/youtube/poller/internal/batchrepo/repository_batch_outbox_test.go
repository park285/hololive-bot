package batchrepo

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestGormBatchRepositoryPersistCommunityPostsConflictWithSentOutboxBackfillsTrackingSentState(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeNotificationDelivery{},
		&domain.YouTubeContentWatermark{},
	)
	repository := NewBatchRepository(db)
	ctx := context.Background()

	publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	detectedAt := publishedAt.Add(20 * time.Second)
	sentAt := detectedAt.Add(40 * time.Second)
	createdAt := publishedAt.Add(-5 * time.Minute)

	require.NoError(t, db.Create(&domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindCommunityPost,
		ChannelID:     "channel-1",
		ContentID:     "post-1",
		Payload:       `{"canonical_post_id":"community:post-1","post_id":"post-1"}`,
		Status:        domain.OutboxStatusSent,
		AttemptCount:  1,
		NextAttemptAt: createdAt,
		CreatedAt:     createdAt,
		SentAt:        &sentAt,
	}).Error)

	post := &domain.YouTubeCommunityPost{
		PostID:        "post-1",
		ChannelID:     "channel-1",
		AuthorName:    "author",
		ContentText:   "hello",
		PublishedText: "1 hour ago",
		PublishedAt:   &publishedAt,
		LikeCount:     10,
		CommentCount:  2,
	}
	trackingRows := []*domain.YouTubeContentAlarmTracking{{
		Kind:               domain.OutboxKindCommunityPost,
		ContentID:          "post-1",
		CanonicalContentID: "community:post-1",
		ChannelID:          "channel-1",
		ActualPublishedAt:  &publishedAt,
		DetectedAt:         detectedAt,
	}}

	err := repository.PersistCommunityPosts(ctx, []*domain.YouTubeCommunityPost{post}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindCommunityPost,
		ChannelID: "channel-1",
		ContentID: "post-1",
		Payload:   buildCommunityNotificationPayload(post, post.PostID),
		Status:    domain.OutboxStatusPending,
	}}, trackingRows, nil)
	require.NoError(t, err)

	var trackingRow domain.YouTubeContentAlarmTracking
	require.NoError(t, db.First(&trackingRow, "kind = ? AND content_id = ?", domain.OutboxKindCommunityPost, "post-1").Error)
	require.NotNil(t, trackingRow.AlarmSentAt)
	require.Equal(t, sentAt, trackingRow.AlarmSentAt.UTC())
	require.Equal(t, domain.YouTubeContentAlarmDeliveryStatusSent, trackingRow.DeliveryStatus)
}

func TestGormBatchRepositoryPersistCommunityPostsConflictWithSentDeliveryBackfillsTrackingSentState(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeNotificationDelivery{},
		&domain.YouTubeContentWatermark{},
	)
	repository := NewBatchRepository(db)
	ctx := context.Background()

	publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	detectedAt := publishedAt.Add(20 * time.Second)
	sentAt := detectedAt.Add(40 * time.Second)
	createdAt := publishedAt.Add(-5 * time.Minute)
	nextAttemptAt := publishedAt.Add(-1 * time.Minute)

	existingOutbox := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindCommunityPost,
		ChannelID:     "channel-1",
		ContentID:     "post-1",
		Payload:       `{"canonical_post_id":"community:post-1","post_id":"post-1"}`,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  1,
		NextAttemptAt: nextAttemptAt,
		CreatedAt:     createdAt,
	}
	require.NoError(t, db.Create(&existingOutbox).Error)
	require.NoError(t, db.Create(&domain.YouTubeNotificationDelivery{
		OutboxID:      existingOutbox.ID,
		RoomID:        "room-1",
		Status:        domain.OutboxStatusSent,
		AttemptCount:  1,
		NextAttemptAt: nextAttemptAt,
		CreatedAt:     createdAt,
		SentAt:        &sentAt,
	}).Error)

	post := &domain.YouTubeCommunityPost{
		PostID:        "post-1",
		ChannelID:     "channel-1",
		AuthorName:    "author",
		ContentText:   "hello",
		PublishedText: "1 hour ago",
		PublishedAt:   &publishedAt,
		LikeCount:     10,
		CommentCount:  2,
	}
	trackingRows := []*domain.YouTubeContentAlarmTracking{{
		Kind:               domain.OutboxKindCommunityPost,
		ContentID:          "post-1",
		CanonicalContentID: "community:post-1",
		ChannelID:          "channel-1",
		ActualPublishedAt:  &publishedAt,
		DetectedAt:         detectedAt,
	}}

	err := repository.PersistCommunityPosts(ctx, []*domain.YouTubeCommunityPost{post}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindCommunityPost,
		ChannelID: "channel-1",
		ContentID: "post-1",
		Payload:   buildCommunityNotificationPayload(post, post.PostID),
		Status:    domain.OutboxStatusPending,
	}}, trackingRows, nil)
	require.NoError(t, err)

	var trackingRow domain.YouTubeContentAlarmTracking
	require.NoError(t, db.First(&trackingRow, "kind = ? AND content_id = ?", domain.OutboxKindCommunityPost, "post-1").Error)
	require.NotNil(t, trackingRow.AlarmSentAt)
	require.Equal(t, sentAt, trackingRow.AlarmSentAt.UTC())
	require.Equal(t, domain.YouTubeContentAlarmDeliveryStatusSent, trackingRow.DeliveryStatus)
}

func TestGormBatchRepositoryPersistVideosConflictWithSentOutboxBackfillsTrackingSentState(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeNotificationDelivery{},
		&domain.YouTubeContentWatermark{},
	)
	repository := NewBatchRepository(db)
	ctx := context.Background()

	publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	detectedAt := publishedAt.Add(20 * time.Second)
	sentAt := detectedAt.Add(40 * time.Second)
	createdAt := publishedAt.Add(-5 * time.Minute)
	shortVideo := &domain.YouTubeVideo{
		VideoID:     "video-1",
		ChannelID:   "channel-1",
		Title:       "title-short-1",
		IsShort:     true,
		PublishedAt: &publishedAt,
		ViewCount:   42,
	}

	require.NoError(t, db.Create(&domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewShort,
		ChannelID:     "channel-1",
		ContentID:     "video-1",
		Payload:       buildShortNotificationPayload(shortVideo, "video-1"),
		Status:        domain.OutboxStatusSent,
		AttemptCount:  1,
		NextAttemptAt: createdAt,
		CreatedAt:     createdAt,
		SentAt:        &sentAt,
	}).Error)

	err := repository.PersistVideos(ctx,
		[]*domain.YouTubeVideo{shortVideo},
		[]*domain.YouTubeNotificationOutbox{{
			Kind:      domain.OutboxKindNewShort,
			ChannelID: "channel-1",
			ContentID: "short:video-1",
			Payload:   buildShortNotificationPayload(shortVideo, "short:video-1"),
			Status:    domain.OutboxStatusPending,
		}},
		[]*domain.YouTubeContentAlarmTracking{{
			Kind:              domain.OutboxKindNewShort,
			ContentID:         "short:video-1",
			ChannelID:         "channel-1",
			ActualPublishedAt: &publishedAt,
			DetectedAt:        detectedAt,
		}},
		nil,
	)
	require.NoError(t, err)

	var trackingRow domain.YouTubeContentAlarmTracking
	require.NoError(t, db.First(&trackingRow, "kind = ? AND content_id = ?", domain.OutboxKindNewShort, "video-1").Error)
	require.Equal(t, "short:video-1", trackingRow.CanonicalContentID)
	require.NotNil(t, trackingRow.AlarmSentAt)
	require.Equal(t, sentAt, trackingRow.AlarmSentAt.UTC())
	require.Equal(t, domain.YouTubeContentAlarmDeliveryStatusSent, trackingRow.DeliveryStatus)
}

func TestGormBatchRepositoryPersistVideosDoesNotReactivateFailedOutboxForNonTargetKinds(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	repository := NewBatchRepository(db)
	ctx := context.Background()

	createdAt := time.Date(2026, 4, 10, 1, 0, 0, 0, time.UTC)
	nextAttemptAt := createdAt.Add(5 * time.Minute)
	existingOutbox := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewVideo,
		ChannelID:     "channel-1",
		ContentID:     "video-1",
		Payload:       `{"video_id":"video-1","version":"old"}`,
		Status:        domain.OutboxStatusFailed,
		AttemptCount:  3,
		NextAttemptAt: nextAttemptAt,
		CreatedAt:     createdAt,
		Error:         "video failed",
	}
	require.NoError(t, db.Create(&existingOutbox).Error)

	err := persistVideos(repository, ctx, []*domain.YouTubeVideo{{
		VideoID:   "video-1",
		ChannelID: "channel-1",
		Title:     "title-video-1",
		ViewCount: 999,
	}}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindNewVideo,
		ChannelID: "channel-1",
		ContentID: "video-1",
		Payload:   `{"video_id":"video-1","version":"new"}`,
		Status:    domain.OutboxStatusPending,
	}}, &domain.YouTubeContentWatermark{
		ChannelID:     "channel-1",
		WatermarkType: domain.WatermarkTypeVideo,
		Initialized:   true,
		LastContentID: "video-1",
	})
	require.NoError(t, err)

	var outboxRows []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	require.Equal(t, existingOutbox.ID, outboxRows[0].ID)
	require.Equal(t, domain.OutboxStatusFailed, outboxRows[0].Status)
	require.Equal(t, 3, outboxRows[0].AttemptCount)
	require.Equal(t, nextAttemptAt, outboxRows[0].NextAttemptAt.UTC())
	require.Equal(t, "video failed", outboxRows[0].Error)
	require.Contains(t, outboxRows[0].Payload, `"version":"old"`)
}

func TestGormBatchRepositoryPersistVideosRollsBackOnNotificationError(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeContentWatermark{},
	)
	repository := NewBatchRepository(db)
	ctx := context.Background()

	err := persistVideos(repository, ctx, []*domain.YouTubeVideo{{
		VideoID:   "video-1",
		ChannelID: "channel-1",
		Title:     "title",
		ViewCount: 1,
	}}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindNewVideo,
		ChannelID: "channel-1",
		ContentID: "video-1",
		Payload:   `{"video_id":"video-1"}`,
		Status:    domain.OutboxStatusPending,
	}}, &domain.YouTubeContentWatermark{
		ChannelID:     "channel-1",
		WatermarkType: domain.WatermarkTypeVideo,
		Initialized:   true,
		LastContentID: "video-1",
	})
	require.Error(t, err)

	var videoCount int64
	require.NoError(t, db.Model(&domain.YouTubeVideo{}).Count(&videoCount).Error)
	require.Zero(t, videoCount)

	var watermarkCount int64
	require.NoError(t, db.Model(&domain.YouTubeContentWatermark{}).Count(&watermarkCount).Error)
	require.Zero(t, watermarkCount)
}

func TestGormBatchRepositoryPersistVideosRejectsBlankNotificationContentID(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	repository := NewBatchRepository(db)
	ctx := context.Background()

	err := persistVideos(repository, ctx, []*domain.YouTubeVideo{{
		VideoID:   "video-1",
		ChannelID: "channel-1",
		Title:     "title",
		ViewCount: 1,
	}}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindNewShort,
		ChannelID: "channel-1",
		ContentID: "   ",
		Payload:   `{"video_id":"video-1"}`,
		Status:    domain.OutboxStatusPending,
	}}, &domain.YouTubeContentWatermark{
		ChannelID:     "channel-1",
		WatermarkType: domain.WatermarkTypeShort,
		Initialized:   true,
		LastContentID: "video-1",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "dedupe key")

	var videoCount int64
	require.NoError(t, db.Model(&domain.YouTubeVideo{}).Count(&videoCount).Error)
	require.Zero(t, videoCount)

	var outboxCount int64
	require.NoError(t, db.Model(&domain.YouTubeNotificationOutbox{}).Count(&outboxCount).Error)
	require.Zero(t, outboxCount)
}
