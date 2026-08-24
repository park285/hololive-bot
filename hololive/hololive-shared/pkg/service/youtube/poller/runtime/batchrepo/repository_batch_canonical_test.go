package batchrepo

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestPgxBatchRepositoryPersistCommunityPostsRejectsPublishedAtStorageRuleMismatch(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	repository := NewBatchRepository(db)
	ctx := t.Context()
	publishedAt := time.Date(2026, time.April, 10, 1, 11, 12, 0, time.UTC)

	err := persistCommunityPosts(ctx, repository, []*domain.YouTubeCommunityPost{{
		PostID:        testPostID,
		ChannelID:     testChannelID,
		AuthorName:    testAuthorName,
		ContentText:   testContentText,
		PublishedText: testPublishedText,
		PublishedAt:   &publishedAt,
		LikeCount:     10,
		CommentCount:  2,
	}}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindCommunityPost,
		ChannelID: testChannelID,
		ContentID: testPostID,
		Payload:   `{"canonical_post_id":"community:post-1","post_id":"post-1","published_at":"2026-04-10T10:11:12+09:00"}`,
		Status:    domain.OutboxStatusPending,
	}}, &domain.YouTubeContentWatermark{
		ChannelID:     testChannelID,
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: testPostID,
	})
	require.ErrorContains(t, err, "payload published_at mismatch")

	var postCount int64

	require.NoError(t, db.Model(&domain.YouTubeCommunityPost{}).Count(&postCount).Error)
	require.Zero(t, postCount)

	var outboxCount int64

	require.NoError(t, db.Model(&domain.YouTubeNotificationOutbox{}).Count(&outboxCount).Error)
	require.Zero(t, outboxCount)
}

func TestPgxBatchRepositoryPersistVideosCollectsSourcePostsWithoutTrackingRows(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeContentWatermark{},
	)
	repository := NewBatchRepository(db)
	ctx := t.Context()
	publishedAt := time.Date(2026, time.April, 10, 1, 11, 12, 0, time.UTC)

	err := repository.PersistVideos(ctx, []*domain.YouTubeVideo{{
		VideoID:     testShortID,
		ChannelID:   testChannelID,
		Title:       testShortTitle,
		IsShort:     true,
		PublishedAt: &publishedAt,
	}}, nil, nil, &domain.YouTubeContentWatermark{ChannelID: testChannelID, WatermarkType: domain.WatermarkTypeShort, Initialized: false, LastContentID: testShortID})
	require.NoError(t, err)

	var sourcePost domain.YouTubeCommunityShortsSourcePost

	require.NoError(t, db.First(&sourcePost, "kind = ? AND post_id = ?", domain.OutboxKindNewShort, testCanonicalShortFromShortID).Error)
	require.Equal(t, testChannelID, sourcePost.ChannelID)
	require.NotNil(t, sourcePost.ActualPublishedAt)
	require.Equal(t, publishedAt, sourcePost.ActualPublishedAt.UTC())
	require.False(t, sourcePost.DetectedAt.IsZero())
}

func TestPgxBatchRepositoryPersistVideosRejectsShortCanonicalPostIDMismatch(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	repository := NewBatchRepository(db)
	ctx := t.Context()
	publishedAt := time.Date(2026, time.April, 10, 1, 11, 12, 0, time.UTC)

	err := persistVideos(ctx, repository, []*domain.YouTubeVideo{{
		VideoID:     testShortID,
		ChannelID:   testChannelID,
		Title:       testShortTitle,
		IsShort:     true,
		PublishedAt: &publishedAt,
	}}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindNewShort,
		ChannelID: testChannelID,
		ContentID: testShortID,
		Payload:   `{"canonical_post_id":"short:short-other","video_id":"short-1","published_at":"2026-04-10T01:11:12Z"}`,
		Status:    domain.OutboxStatusPending,
	}}, &domain.YouTubeContentWatermark{ChannelID: testChannelID, WatermarkType: domain.WatermarkTypeShort, Initialized: true, LastContentID: testShortID})
	require.ErrorContains(t, err, "payload canonical_post_id mismatch")
}

func TestPgxBatchRepositoryPersistVideosReusesLegacyRawShortIdentity(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	repository := NewBatchRepository(db)
	ctx := t.Context()
	publishedAt := time.Date(2026, time.April, 10, 1, 11, 12, 0, time.UTC)
	shortVideo := &domain.YouTubeVideo{
		VideoID:     testShortID,
		ChannelID:   testChannelID,
		Title:       testShortTitle,
		IsShort:     true,
		PublishedAt: &publishedAt,
	}

	require.NoError(t, db.Create(&domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewShort,
		ChannelID:     testChannelID,
		ContentID:     testShortID,
		Payload:       buildShortNotificationPayload(shortVideo, testShortID),
		Status:        domain.OutboxStatusPending,
		NextAttemptAt: publishedAt,
		CreatedAt:     publishedAt,
	}).Error)
	require.NoError(t, db.Create(&domain.YouTubeContentAlarmTracking{
		Kind:               domain.OutboxKindNewShort,
		ContentID:          testShortID,
		CanonicalContentID: testCanonicalShortFromShortID,
		ChannelID:          testChannelID,
		ActualPublishedAt:  &publishedAt,
		DetectedAt:         publishedAt,
	}).Error)

	err := repository.PersistVideos(ctx, []*domain.YouTubeVideo{shortVideo}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindNewShort,
		ChannelID: testChannelID,
		ContentID: testCanonicalShortFromShortID,
		Payload:   buildShortNotificationPayload(shortVideo, testCanonicalShortFromShortID),
		Status:    domain.OutboxStatusPending,
	}}, []*domain.YouTubeContentAlarmTracking{{
		Kind:              domain.OutboxKindNewShort,
		ContentID:         testCanonicalShortFromShortID,
		ChannelID:         testChannelID,
		ActualPublishedAt: &publishedAt,
		DetectedAt:        publishedAt.Add(time.Minute),
	}}, &domain.YouTubeContentWatermark{
		ChannelID:     testChannelID,
		WatermarkType: domain.WatermarkTypeShort,
		Initialized:   true,
		LastContentID: testCanonicalShortFromShortID,
	})
	require.NoError(t, err)

	var outboxRows []domain.YouTubeNotificationOutbox

	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	require.Equal(t, testShortID, outboxRows[0].ContentID)

	var trackingRows []domain.YouTubeContentAlarmTracking

	require.NoError(t, db.Order("content_id ASC").Find(&trackingRows).Error)
	require.Len(t, trackingRows, 1)
	require.Equal(t, testShortID, trackingRows[0].ContentID)

	var watermark domain.YouTubeContentWatermark

	require.NoError(t, db.Where("channel_id = ? AND watermark_type = ?", testChannelID, domain.WatermarkTypeShort).First(&watermark).Error)
	require.Equal(t, testCanonicalShortFromShortID, watermark.LastContentID)
}

func TestPgxBatchRepositoryPersistCommunityPostsRejectsCanonicalPostIDMismatch(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	repository := NewBatchRepository(db)
	ctx := t.Context()
	publishedAt := time.Date(2026, time.April, 10, 1, 11, 12, 0, time.UTC)

	err := persistCommunityPosts(ctx, repository, []*domain.YouTubeCommunityPost{{
		PostID:        testPostID,
		ChannelID:     testChannelID,
		AuthorName:    testAuthorName,
		ContentText:   testContentText,
		PublishedText: testPublishedText,
		PublishedAt:   &publishedAt,
		LikeCount:     10,
		CommentCount:  2,
	}}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindCommunityPost,
		ChannelID: testChannelID,
		ContentID: testPostID,
		Payload:   `{"canonical_post_id":"community:post-other","post_id":"post-1","published_at":"2026-04-10T01:11:12Z"}`,
		Status:    domain.OutboxStatusPending,
	}}, &domain.YouTubeContentWatermark{
		ChannelID:     testChannelID,
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: testPostID,
	})
	require.ErrorContains(t, err, "payload canonical_post_id mismatch")
}

func TestPgxBatchRepositoryPersistCommunityPostsBackfillsPublishedAt(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	repository := NewBatchRepository(db)
	ctx := t.Context()

	err := persistCommunityPosts(ctx, repository, []*domain.YouTubeCommunityPost{{
		PostID:        testPostID,
		ChannelID:     testChannelID,
		AuthorName:    testAuthorName,
		ContentText:   testContentText,
		PublishedText: testPublishedText,
		LikeCount:     10,
		CommentCount:  2,
	}}, nil, &domain.YouTubeContentWatermark{
		ChannelID:     testChannelID,
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: testPostID,
	})
	require.NoError(t, err)

	publishedAt := time.Date(2026, time.April, 10, 1, 11, 12, 0, time.UTC)

	err = persistCommunityPosts(ctx, repository, []*domain.YouTubeCommunityPost{{
		PostID:        testPostID,
		ChannelID:     testChannelID,
		AuthorName:    testAuthorName,
		ContentText:   testContentText,
		PublishedText: testPublishedText,
		PublishedAt:   &publishedAt,
		LikeCount:     11,
		CommentCount:  3,
	}}, nil, &domain.YouTubeContentWatermark{
		ChannelID:     testChannelID,
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: testPostID,
	})
	require.NoError(t, err)

	var post domain.YouTubeCommunityPost

	require.NoError(t, db.First(&post, "post_id = ?", testPostID).Error)
	require.NotNil(t, post.PublishedAt)
	require.Equal(t, publishedAt, post.PublishedAt.UTC())
	require.EqualValues(t, 11, post.LikeCount)
	require.EqualValues(t, 3, post.CommentCount)
}

func TestPgxBatchRepositoryPersistCommunityPostsReactivatesFailedOutboxAndFailedDeliveries(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeNotificationDelivery{},
		&domain.YouTubeContentWatermark{},
	)
	repository := NewBatchRepository(db)
	ctx := t.Context()

	publishedAt := time.Date(2026, time.April, 10, 1, 11, 12, 0, time.UTC)
	createdAt := publishedAt.Add(-15 * time.Minute)
	nextAttemptAt := publishedAt.Add(-2 * time.Minute)
	sentAt := publishedAt.Add(-time.Minute)

	existingOutbox := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindCommunityPost,
		ChannelID:     testChannelID,
		ContentID:     testPostID,
		Payload:       `{"canonical_post_id":"community:post-1","post_id":"post-1","published_at":"2026-04-10T01:11:12Z","version":"old"}`,
		Status:        domain.OutboxStatusFailed,
		AttemptCount:  3,
		NextAttemptAt: nextAttemptAt,
		CreatedAt:     createdAt,
		Error:         "old failed",
	}
	require.NoError(t, db.Create(&existingOutbox).Error)
	require.NoError(t, db.Create([]domain.YouTubeNotificationDelivery{
		{
			OutboxID:      existingOutbox.ID,
			RoomID:        "room-failed",
			Status:        domain.OutboxStatusFailed,
			AttemptCount:  3,
			NextAttemptAt: nextAttemptAt,
			CreatedAt:     createdAt,
			Error:         "delivery failed",
		},
		{
			OutboxID:      existingOutbox.ID,
			RoomID:        "room-sent",
			Status:        domain.OutboxStatusSent,
			AttemptCount:  1,
			NextAttemptAt: nextAttemptAt,
			CreatedAt:     createdAt,
			SentAt:        &sentAt,
		},
	}).Error)

	post := &domain.YouTubeCommunityPost{
		PostID:        testPostID,
		ChannelID:     testChannelID,
		AuthorName:    testAuthorName,
		ContentText:   testContentText,
		PublishedText: testPublishedText,
		PublishedAt:   &publishedAt,
		LikeCount:     10,
		CommentCount:  2,
	}

	err := persistCommunityPosts(ctx, repository, []*domain.YouTubeCommunityPost{post}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindCommunityPost,
		ChannelID: testChannelID,
		ContentID: testPostID,
		Payload:   buildCommunityNotificationPayload(post, post.PostID),
		Status:    domain.OutboxStatusPending,
	}}, &domain.YouTubeContentWatermark{
		ChannelID:     testChannelID,
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: testPostID,
	})
	require.NoError(t, err)

	requireReactivatedOutboxAndDeliveries(t, db, existingOutbox.ID, nextAttemptAt, sentAt)
}

func TestPgxBatchRepositoryPersistCommunityPostsFinalizesFailedOutboxWhenTrackingAlreadySent(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeNotificationDelivery{},
		&domain.YouTubeContentWatermark{},
	)
	repository := NewBatchRepository(db)
	ctx := t.Context()

	publishedAt := time.Date(2026, time.April, 10, 1, 11, 12, 0, time.UTC)
	detectedAt := publishedAt.Add(20 * time.Second)
	createdAt := publishedAt.Add(-15 * time.Minute)
	nextAttemptAt := publishedAt.Add(-2 * time.Minute)
	sentAt := detectedAt.Add(40 * time.Second)

	existingOutbox := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindCommunityPost,
		ChannelID:     testChannelID,
		ContentID:     testPostID,
		Payload:       `{"canonical_post_id":"community:post-1","post_id":"post-1","version":"old"}`,
		Status:        domain.OutboxStatusFailed,
		AttemptCount:  3,
		NextAttemptAt: nextAttemptAt,
		CreatedAt:     createdAt,
		Error:         "old failed",
	}
	require.NoError(t, db.Create(&existingOutbox).Error)
	require.NoError(t, db.Create(&domain.YouTubeNotificationDelivery{
		OutboxID:      existingOutbox.ID,
		RoomID:        "room-failed",
		Status:        domain.OutboxStatusFailed,
		AttemptCount:  3,
		NextAttemptAt: nextAttemptAt,
		CreatedAt:     createdAt,
		Error:         "delivery failed",
	}).Error)
	require.NoError(t, db.Create(&domain.YouTubeContentAlarmTracking{
		Kind:               domain.OutboxKindCommunityPost,
		ContentID:          testPostID,
		CanonicalContentID: "community:post-1",
		ChannelID:          testChannelID,
		ActualPublishedAt:  &publishedAt,
		DetectedAt:         detectedAt,
		AlarmSentAt:        &sentAt,
		DeliveryStatus:     domain.YouTubeContentAlarmDeliveryStatusSent,
	}).Error)

	post := &domain.YouTubeCommunityPost{
		PostID:        testPostID,
		ChannelID:     testChannelID,
		AuthorName:    testAuthorName,
		ContentText:   testContentText,
		PublishedText: testPublishedText,
		PublishedAt:   &publishedAt,
		LikeCount:     10,
		CommentCount:  2,
	}

	err := persistCommunityPosts(ctx, repository, []*domain.YouTubeCommunityPost{post}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindCommunityPost,
		ChannelID: testChannelID,
		ContentID: testPostID,
		Payload:   buildCommunityNotificationPayload(post, post.PostID),
		Status:    domain.OutboxStatusPending,
	}}, &domain.YouTubeContentWatermark{
		ChannelID:     testChannelID,
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: testPostID,
	})
	require.NoError(t, err)

	requireFinalizedSentOutboxAndDelivery(t, db, sentAt, 3)
}

func TestPgxBatchRepositoryPersistVideosFinalizesFailedOutboxWhenAlarmStateAlreadySent(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeNotificationDelivery{},
		&domain.YouTubeContentWatermark{},
	)
	repository := NewBatchRepository(db)
	ctx := t.Context()

	publishedAt := time.Date(2026, time.April, 10, 1, 11, 12, 0, time.UTC)
	detectedAt := publishedAt.Add(20 * time.Second)
	createdAt := publishedAt.Add(-15 * time.Minute)
	nextAttemptAt := publishedAt.Add(-2 * time.Minute)
	sentAt := detectedAt.Add(40 * time.Second)
	shortVideo := &domain.YouTubeVideo{
		VideoID:     testVideoID,
		ChannelID:   testChannelID,
		Title:       testShortTitle,
		IsShort:     true,
		PublishedAt: &publishedAt,
		ViewCount:   42,
	}

	existingOutbox := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewShort,
		ChannelID:     testChannelID,
		ContentID:     testVideoID,
		Payload:       buildShortNotificationPayload(shortVideo, testVideoID),
		Status:        domain.OutboxStatusFailed,
		AttemptCount:  2,
		NextAttemptAt: nextAttemptAt,
		CreatedAt:     createdAt,
		Error:         "old failed",
	}
	require.NoError(t, repository.PersistVideos(ctx, []*domain.YouTubeVideo{shortVideo}, nil, nil, nil))
	require.NoError(t, db.Create(&existingOutbox).Error)
	require.NoError(t, db.Create(&domain.YouTubeNotificationDelivery{
		OutboxID:      existingOutbox.ID,
		RoomID:        "room-shorts",
		Status:        domain.OutboxStatusFailed,
		AttemptCount:  2,
		NextAttemptAt: nextAttemptAt,
		CreatedAt:     createdAt,
		Error:         "delivery failed",
	}).Error)
	require.NoError(t, db.Create(&domain.YouTubeCommunityShortsAlarmState{
		Kind:              domain.OutboxKindNewShort,
		PostID:            testCanonicalShortFromVideoID,
		ContentID:         testVideoID,
		ChannelID:         testChannelID,
		ActualPublishedAt: &publishedAt,
		DetectedAt:        detectedAt,
		AlarmSentAt:       &sentAt,
		DeliveryStatus:    domain.YouTubeCommunityShortsAlarmStateStatusSent,
	}).Error)

	err := repository.PersistVideos(ctx,
		[]*domain.YouTubeVideo{shortVideo},
		[]*domain.YouTubeNotificationOutbox{{
			Kind:      domain.OutboxKindNewShort,
			ChannelID: testChannelID,
			ContentID: testCanonicalShortFromVideoID,
			Payload:   buildShortNotificationPayload(shortVideo, testCanonicalShortFromVideoID),
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
	)
	require.NoError(t, err)

	requireFinalizedSentOutboxAndDelivery(t, db, sentAt, 2)
}

func requireReactivatedOutboxAndDeliveries(t *testing.T, db *batchTestDB, outboxID int64, nextAttemptAt, sentAt time.Time) {
	t.Helper()

	var outboxRows []domain.YouTubeNotificationOutbox

	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	require.Equal(t, outboxID, outboxRows[0].ID)
	require.Equal(t, domain.OutboxStatusPending, outboxRows[0].Status)
	require.Zero(t, outboxRows[0].AttemptCount)
	require.True(t, outboxRows[0].NextAttemptAt.After(nextAttemptAt))
	require.Empty(t, outboxRows[0].Error)
	require.Nil(t, outboxRows[0].LockedAt)
	require.Nil(t, outboxRows[0].SentAt)
	require.Contains(t, outboxRows[0].Payload, `"canonical_post_id": "community:post-1"`)

	var deliveryRows []domain.YouTubeNotificationDelivery

	require.NoError(t, db.Order("room_id ASC").Find(&deliveryRows).Error)
	require.Len(t, deliveryRows, 2)

	require.Equal(t, "room-failed", deliveryRows[0].RoomID)
	require.Equal(t, domain.OutboxStatusPending, deliveryRows[0].Status)
	require.Zero(t, deliveryRows[0].AttemptCount)
	require.True(t, deliveryRows[0].NextAttemptAt.After(nextAttemptAt))
	require.Empty(t, deliveryRows[0].Error)
	require.Nil(t, deliveryRows[0].LockedAt)
	require.Nil(t, deliveryRows[0].SentAt)

	require.Equal(t, "room-sent", deliveryRows[1].RoomID)
	require.Equal(t, domain.OutboxStatusSent, deliveryRows[1].Status)
	require.Equal(t, 1, deliveryRows[1].AttemptCount)
	require.NotNil(t, deliveryRows[1].SentAt)
	require.Equal(t, sentAt, deliveryRows[1].SentAt.UTC())
}

func requireFinalizedSentOutboxAndDelivery(t *testing.T, db *batchTestDB, sentAt time.Time, attemptCount int) {
	t.Helper()

	var outboxRows []domain.YouTubeNotificationOutbox

	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	require.Equal(t, domain.OutboxStatusSent, outboxRows[0].Status)
	require.NotNil(t, outboxRows[0].SentAt)
	require.Equal(t, sentAt, outboxRows[0].SentAt.UTC())
	require.Empty(t, outboxRows[0].Error)
	require.Equal(t, attemptCount, outboxRows[0].AttemptCount)

	var deliveryRows []domain.YouTubeNotificationDelivery

	require.NoError(t, db.Find(&deliveryRows).Error)
	require.Len(t, deliveryRows, 1)
	require.Equal(t, domain.OutboxStatusSent, deliveryRows[0].Status)
	require.NotNil(t, deliveryRows[0].SentAt)
	require.Equal(t, sentAt, deliveryRows[0].SentAt.UTC())
	require.Empty(t, deliveryRows[0].Error)
	require.Equal(t, attemptCount, deliveryRows[0].AttemptCount)
}
