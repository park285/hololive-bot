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
