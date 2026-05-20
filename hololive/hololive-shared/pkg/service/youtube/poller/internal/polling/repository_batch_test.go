// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package polling

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/kapu/hololive-shared/pkg/domain"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

func TestGormBatchRepositoryPersistVideos(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	repo := newBatchRepository(db)
	ctx := context.Background()

	videos := make([]*domain.YouTubeVideo, 0, pollerBatchMaxSize+5)
	notifications := make([]*domain.YouTubeNotificationOutbox, 0, pollerBatchMaxSize+5)
	for i := range pollerBatchMaxSize + 5 {
		videoID := fmt.Sprintf("video-%03d", i)
		videos = append(videos, &domain.YouTubeVideo{
			VideoID:   videoID,
			ChannelID: "channel-1",
			Title:     "title-" + videoID,
			IsShort:   i%2 == 0,
			ViewCount: int64(100 + i),
		})
		notifications = append(notifications, &domain.YouTubeNotificationOutbox{
			Kind:      domain.OutboxKindNewVideo,
			ChannelID: "channel-1",
			ContentID: videoID,
			Payload:   `{"video_id":"` + videoID + `"}`,
			Status:    domain.OutboxStatusPending,
		})
	}

	err := persistVideos(repo, ctx, videos, notifications, &domain.YouTubeContentWatermark{
		ChannelID:     "channel-1",
		WatermarkType: domain.WatermarkTypeVideo,
		Initialized:   true,
		LastContentID: "video-054",
	})
	require.NoError(t, err)

	var videoCount int64
	require.NoError(t, db.Model(&domain.YouTubeVideo{}).Count(&videoCount).Error)
	require.EqualValues(t, pollerBatchMaxSize+5, videoCount)

	var outboxCount int64
	require.NoError(t, db.Model(&domain.YouTubeNotificationOutbox{}).Count(&outboxCount).Error)
	require.EqualValues(t, pollerBatchMaxSize+5, outboxCount)

	var watermark domain.YouTubeContentWatermark
	require.NoError(t, db.Where("channel_id = ? AND watermark_type = ?", "channel-1", domain.WatermarkTypeVideo).First(&watermark).Error)
	require.Equal(t, "video-054", watermark.LastContentID)

	err = persistVideos(repo, ctx, []*domain.YouTubeVideo{{
		VideoID:   "video-000",
		ChannelID: "channel-1",
		Title:     "title-video-000",
		ViewCount: 999,
	}}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindNewVideo,
		ChannelID: "channel-1",
		ContentID: "video-000",
		Payload:   `{"video_id":"video-000"}`,
		Status:    domain.OutboxStatusPending,
	}}, &domain.YouTubeContentWatermark{
		ChannelID:     "channel-1",
		WatermarkType: domain.WatermarkTypeVideo,
		Initialized:   true,
		LastContentID: "video-000",
	})
	require.NoError(t, err)

	var updated domain.YouTubeVideo
	require.NoError(t, db.First(&updated, "video_id = ?", "video-000").Error)
	require.EqualValues(t, 999, updated.ViewCount)

	require.NoError(t, db.Model(&domain.YouTubeNotificationOutbox{}).Count(&outboxCount).Error)
	require.EqualValues(t, pollerBatchMaxSize+5, outboxCount)
}

func TestGormBatchRepositoryPersistVideosAllowsDifferentKindsForSameContentID(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	repo := newBatchRepository(db)
	ctx := context.Background()

	videoSuccessBefore := testutil.ToFloat64(outboxInsertTotal.WithLabelValues(string(domain.OutboxKindNewVideo), "success"))
	videoConflictBefore := testutil.ToFloat64(outboxInsertTotal.WithLabelValues(string(domain.OutboxKindNewVideo), "conflict"))
	shortSuccessBefore := testutil.ToFloat64(outboxInsertTotal.WithLabelValues(string(domain.OutboxKindNewShort), "success"))
	notifications := []*domain.YouTubeNotificationOutbox{
		{
			Kind:      domain.OutboxKindNewVideo,
			ChannelID: "channel-1",
			ContentID: "video-1",
			Payload:   `{"video_id":"video-1","kind":"video"}`,
			Status:    domain.OutboxStatusPending,
		},
		{
			Kind:      domain.OutboxKindNewShort,
			ChannelID: "channel-1",
			ContentID: "video-1",
			Payload:   `{"canonical_post_id":"short:video-1","video_id":"video-1","kind":"short"}`,
			Status:    domain.OutboxStatusPending,
		},
		{
			Kind:      domain.OutboxKindNewVideo,
			ChannelID: "channel-1",
			ContentID: "video-1",
			Payload:   `{"video_id":"video-1","kind":"video-duplicate"}`,
			Status:    domain.OutboxStatusPending,
		},
	}

	err := persistVideos(repo, ctx, []*domain.YouTubeVideo{{
		VideoID:   "video-1",
		ChannelID: "channel-1",
		Title:     "title-video-1",
		ViewCount: 1,
	}}, notifications, &domain.YouTubeContentWatermark{
		ChannelID:     "channel-1",
		WatermarkType: domain.WatermarkTypeVideo,
		Initialized:   true,
		LastContentID: "video-1",
	})
	require.NoError(t, err)

	var outboxCount int64
	require.NoError(t, db.Model(&domain.YouTubeNotificationOutbox{}).Count(&outboxCount).Error)
	require.EqualValues(t, 2, outboxCount)
	require.Equal(t, videoSuccessBefore+1, testutil.ToFloat64(outboxInsertTotal.WithLabelValues(string(domain.OutboxKindNewVideo), "success")))
	require.Equal(t, videoConflictBefore, testutil.ToFloat64(outboxInsertTotal.WithLabelValues(string(domain.OutboxKindNewVideo), "conflict")))
	require.Equal(t, shortSuccessBefore+1, testutil.ToFloat64(outboxInsertTotal.WithLabelValues(string(domain.OutboxKindNewShort), "success")))
}

func TestGormBatchRepositoryPersistVideosConcurrentOutboxInsertIsIdempotent(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	require.NoError(t, db.Exec("PRAGMA busy_timeout = 5000").Error)
	ctx := context.Background()

	runPersist := func() error {
		repo := newBatchRepository(db)
		return persistVideos(repo, ctx, []*domain.YouTubeVideo{{
			VideoID:   "video-race",
			ChannelID: "channel-1",
			Title:     "title-video-race",
			ViewCount: 1,
		}}, []*domain.YouTubeNotificationOutbox{{
			Kind:      domain.OutboxKindNewVideo,
			ChannelID: "channel-1",
			ContentID: "video-race",
			Payload:   `{"video_id":"video-race"}`,
			Status:    domain.OutboxStatusPending,
		}}, &domain.YouTubeContentWatermark{
			ChannelID:     "channel-1",
			WatermarkType: domain.WatermarkTypeVideo,
			Initialized:   true,
			LastContentID: "video-race",
		})
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			errs <- runPersist()
		})
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	var outboxCount int64
	require.NoError(t, db.Model(&domain.YouTubeNotificationOutbox{}).
		Where("kind = ? AND content_id = ?", domain.OutboxKindNewVideo, "video-race").
		Count(&outboxCount).Error)
	require.EqualValues(t, 1, outboxCount)
}

func TestGormBatchRepositoryPersistVideosPrimaryAndBackfillSameContentLeaveOneOutboxRow(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	repo := newBatchRepository(db)
	ctx := context.Background()
	video := &domain.YouTubeVideo{
		VideoID:   "short-backfill",
		ChannelID: "channel-1",
		Title:     "title-short-backfill",
		IsShort:   true,
		ViewCount: 1,
	}
	watermark := &domain.YouTubeContentWatermark{
		ChannelID:     "channel-1",
		WatermarkType: domain.WatermarkTypeShort,
		Initialized:   true,
		LastContentID: "short-backfill",
	}

	require.NoError(t, persistVideos(repo, ctx, []*domain.YouTubeVideo{video}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindNewShort,
		ChannelID: "channel-1",
		ContentID: "short:short-backfill",
		Payload:   buildShortNotificationPayload(video, "short:short-backfill"),
		Status:    domain.OutboxStatusPending,
	}}, watermark))
	require.NoError(t, persistVideos(repo, ctx, []*domain.YouTubeVideo{video}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindNewShort,
		ChannelID: "channel-1",
		ContentID: "short:short-backfill",
		Payload:   buildShortNotificationPayload(video, "short:short-backfill"),
		Status:    domain.OutboxStatusPending,
	}}, watermark))

	var outboxCount int64
	require.NoError(t, db.Model(&domain.YouTubeNotificationOutbox{}).
		Where("kind = ? AND content_id = ?", domain.OutboxKindNewShort, "short:short-backfill").
		Count(&outboxCount).Error)
	require.EqualValues(t, 1, outboxCount)
}

func TestGormBatchRepositoryPersistVideosPersistsShortPublishedAt(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	repo := newBatchRepository(db)
	ctx := context.Background()
	rawPublishedAt := time.Date(2026, 4, 10, 10, 11, 12, 123000000, time.FixedZone("KST", 9*60*60))
	canonicalPublishedAt := yttimestamp.Normalize(rawPublishedAt)
	shortVideo := &domain.YouTubeVideo{
		VideoID:     "short-1",
		ChannelID:   "channel-1",
		Title:       "title-short-1",
		IsShort:     true,
		PublishedAt: &canonicalPublishedAt,
		ViewCount:   42,
	}

	err := persistVideos(repo, ctx, []*domain.YouTubeVideo{shortVideo}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindNewShort,
		ChannelID: "channel-1",
		ContentID: "short-1",
		Payload:   buildShortNotificationPayload(shortVideo, shortVideo.VideoID),
		Status:    domain.OutboxStatusPending,
	}}, &domain.YouTubeContentWatermark{
		ChannelID:     "channel-1",
		WatermarkType: domain.WatermarkTypeShort,
		Initialized:   true,
		LastContentID: "short:short-1",
	})
	require.NoError(t, err)

	var stored domain.YouTubeVideo
	require.NoError(t, db.First(&stored, "video_id = ?", "short-1").Error)
	require.NotNil(t, stored.PublishedAt)
	require.Equal(t, yttimestamp.Format(canonicalPublishedAt), stored.PublishedAt.Format(time.RFC3339Nano))

	var outbox domain.YouTubeNotificationOutbox
	require.NoError(t, db.First(&outbox, "kind = ? AND content_id = ?", domain.OutboxKindNewShort, "short:short-1").Error)
	require.Contains(t, outbox.Payload, `"canonical_post_id":"short:short-1"`)
	require.Contains(t, outbox.Payload, `"published_at":"`+yttimestamp.Format(canonicalPublishedAt)+`"`)

	var watermark domain.YouTubeContentWatermark
	require.NoError(t, db.Where("channel_id = ? AND watermark_type = ?", "channel-1", domain.WatermarkTypeShort).First(&watermark).Error)
	require.Equal(t, "short:short-1", watermark.LastContentID)
}

func TestGormBatchRepositoryPersistVideosPreservesExistingPublishedAt(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	repo := newBatchRepository(db)
	ctx := context.Background()
	firstPublishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	laterPublishedAt := firstPublishedAt.Add(5 * time.Minute)

	require.NoError(t, persistVideos(repo, ctx, []*domain.YouTubeVideo{{
		VideoID:     "short-stable",
		ChannelID:   "channel-1",
		Title:       "title-short-stable",
		IsShort:     true,
		PublishedAt: &firstPublishedAt,
		ViewCount:   42,
	}}, nil, &domain.YouTubeContentWatermark{
		ChannelID:     "channel-1",
		WatermarkType: domain.WatermarkTypeShort,
		Initialized:   true,
		LastContentID: "short:short-stable",
	}))
	require.NoError(t, persistVideos(repo, ctx, []*domain.YouTubeVideo{{
		VideoID:     "short-stable",
		ChannelID:   "channel-1",
		Title:       "title-short-stable",
		IsShort:     true,
		PublishedAt: &laterPublishedAt,
		ViewCount:   43,
	}}, nil, &domain.YouTubeContentWatermark{
		ChannelID:     "channel-1",
		WatermarkType: domain.WatermarkTypeShort,
		Initialized:   true,
		LastContentID: "short:short-stable",
	}))

	var stored domain.YouTubeVideo
	require.NoError(t, db.First(&stored, "video_id = ?", "short-stable").Error)
	require.NotNil(t, stored.PublishedAt)
	require.Equal(t, firstPublishedAt, stored.PublishedAt.UTC())
	require.EqualValues(t, 43, stored.ViewCount)
}

func TestGormBatchRepositoryPersistVideosRejectsShortPublishedAtStorageRuleMismatch(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	repo := newBatchRepository(db)
	ctx := context.Background()
	publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)

	err := persistVideos(repo, ctx, []*domain.YouTubeVideo{{
		VideoID:     "short-1",
		ChannelID:   "channel-1",
		Title:       "title-short-1",
		IsShort:     true,
		PublishedAt: &publishedAt,
	}}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindNewShort,
		ChannelID: "channel-1",
		ContentID: "short-1",
		Payload:   `{"canonical_post_id":"short:short-1","video_id":"short-1","published_at":"2026-04-10T10:11:12+09:00"}`,
		Status:    domain.OutboxStatusPending,
	}}, &domain.YouTubeContentWatermark{ChannelID: "channel-1", WatermarkType: domain.WatermarkTypeShort, Initialized: true, LastContentID: "short-1"})
	require.ErrorContains(t, err, "payload published_at mismatch")
}

func TestGormBatchRepositoryPersistCommunityPosts(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	repo := newBatchRepository(db)
	ctx := context.Background()
	publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	communityPost := &domain.YouTubeCommunityPost{
		PostID:        "post-1",
		ChannelID:     "channel-1",
		AuthorName:    "author",
		ContentText:   "hello",
		PublishedText: "1 hour ago",
		PublishedAt:   &publishedAt,
		LikeCount:     10,
		CommentCount:  2,
	}

	err := persistCommunityPosts(repo, ctx, []*domain.YouTubeCommunityPost{communityPost}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindCommunityPost,
		ChannelID: "channel-1",
		ContentID: "post-1",
		Payload:   buildCommunityNotificationPayload(communityPost, communityPost.PostID),
		Status:    domain.OutboxStatusPending,
	}}, &domain.YouTubeContentWatermark{
		ChannelID:     "channel-1",
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: "post-1",
	})
	require.NoError(t, err)

	var post domain.YouTubeCommunityPost
	require.NoError(t, db.First(&post, "post_id = ?", "post-1").Error)
	require.EqualValues(t, 10, post.LikeCount)
	require.EqualValues(t, 2, post.CommentCount)
	require.NotNil(t, post.PublishedAt)
	require.Equal(t, publishedAt, post.PublishedAt.UTC())

	var outbox domain.YouTubeNotificationOutbox
	require.NoError(t, db.First(&outbox, "kind = ? AND content_id = ?", domain.OutboxKindCommunityPost, "post-1").Error)
	require.Contains(t, outbox.Payload, `"canonical_post_id":"community:post-1"`)
	require.Contains(t, outbox.Payload, `"published_at":"`+publishedAt.Format(time.RFC3339Nano)+`"`)

	var watermark domain.YouTubeContentWatermark
	require.NoError(t, db.Where("channel_id = ? AND watermark_type = ?", "channel-1", domain.WatermarkTypeCommunityPost).First(&watermark).Error)
	require.Equal(t, "post-1", watermark.LastContentID)
}

func TestGormBatchRepositoryPersistCommunityPostsPreservesExistingPublishedAt(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	repo := newBatchRepository(db)
	ctx := context.Background()
	firstPublishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	laterPublishedAt := firstPublishedAt.Add(5 * time.Minute)

	require.NoError(t, persistCommunityPosts(repo, ctx, []*domain.YouTubeCommunityPost{{
		PostID:        "post-stable",
		ChannelID:     "channel-1",
		AuthorName:    "author",
		ContentText:   "hello",
		PublishedText: "1 hour ago",
		PublishedAt:   &firstPublishedAt,
		LikeCount:     10,
		CommentCount:  2,
	}}, nil, &domain.YouTubeContentWatermark{
		ChannelID:     "channel-1",
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: "post-stable",
	}))
	require.NoError(t, persistCommunityPosts(repo, ctx, []*domain.YouTubeCommunityPost{{
		PostID:        "post-stable",
		ChannelID:     "channel-1",
		AuthorName:    "author",
		ContentText:   "hello",
		PublishedText: "1 hour ago",
		PublishedAt:   &laterPublishedAt,
		LikeCount:     11,
		CommentCount:  3,
	}}, nil, &domain.YouTubeContentWatermark{
		ChannelID:     "channel-1",
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: "post-stable",
	}))

	var post domain.YouTubeCommunityPost
	require.NoError(t, db.First(&post, "post_id = ?", "post-stable").Error)
	require.NotNil(t, post.PublishedAt)
	require.Equal(t, firstPublishedAt, post.PublishedAt.UTC())
	require.EqualValues(t, 11, post.LikeCount)
	require.EqualValues(t, 3, post.CommentCount)
}

func TestGormBatchRepositoryPersistCommunityPostsUpsertsAlarmState(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	repo := newBatchRepository(db)
	ctx := context.Background()
	publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	detectedAt := publishedAt.Add(20 * time.Second)

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
		Kind:              domain.OutboxKindCommunityPost,
		ContentID:         "post-1",
		ChannelID:         "channel-1",
		ActualPublishedAt: &publishedAt,
		DetectedAt:        detectedAt,
	}}

	err := repo.PersistCommunityPosts(ctx, []*domain.YouTubeCommunityPost{post}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindCommunityPost,
		ChannelID: "channel-1",
		ContentID: "post-1",
		Payload:   buildCommunityNotificationPayload(post, post.PostID),
		Status:    domain.OutboxStatusPending,
	}}, trackingRows, nil)
	require.NoError(t, err)

	var state domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&state, "kind = ? AND post_id = ?", domain.OutboxKindCommunityPost, "community:post-1").Error)
	require.Equal(t, "post-1", state.ContentID)
	require.Equal(t, "channel-1", state.ChannelID)
	require.NotNil(t, state.ActualPublishedAt)
	require.Equal(t, publishedAt, state.ActualPublishedAt.UTC())
	require.Equal(t, detectedAt, state.DetectedAt.UTC())
	require.Nil(t, state.AuthorizedAt)
	require.Nil(t, state.AlarmSentAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusDetected, state.DeliveryStatus)
}

func TestGormBatchRepositoryPersistCommunityShortsDuplicatePollKeepsExistingClaimState(t *testing.T) {
	testCases := []struct {
		name      string
		kind      domain.OutboxKind
		postID    string
		contentID string
		seed      func(t *testing.T, db *gorm.DB, publishedAt, detectedAt, authorizedAt time.Time)
		persist   func(t *testing.T, repo batchRepository, ctx context.Context, publishedAt, detectedAt time.Time) error
	}{
		{
			name:      "community post",
			kind:      domain.OutboxKindCommunityPost,
			postID:    "community:post-duplicate",
			contentID: "post-duplicate",
			seed: func(t *testing.T, db *gorm.DB, publishedAt, detectedAt, authorizedAt time.Time) {
				t.Helper()

				post := &domain.YouTubeCommunityPost{
					PostID:        "post-duplicate",
					ChannelID:     "channel-1",
					AuthorName:    "author",
					ContentText:   "hello",
					PublishedText: "1 hour ago",
					PublishedAt:   &publishedAt,
					LikeCount:     10,
					CommentCount:  2,
				}
				require.NoError(t, db.Create(&domain.YouTubeNotificationOutbox{
					Kind:          domain.OutboxKindCommunityPost,
					ChannelID:     "channel-1",
					ContentID:     "post-duplicate",
					Payload:       buildCommunityNotificationPayload(post, post.PostID),
					Status:        domain.OutboxStatusPending,
					NextAttemptAt: authorizedAt,
					CreatedAt:     authorizedAt,
				}).Error)
				require.NoError(t, db.Create(&domain.YouTubeCommunityShortsAlarmState{
					Kind:              domain.OutboxKindCommunityPost,
					PostID:            "community:post-duplicate",
					ContentID:         "post-duplicate",
					ChannelID:         "channel-1",
					ActualPublishedAt: &publishedAt,
					DetectedAt:        detectedAt,
					AuthorizedAt:      &authorizedAt,
					DeliveryStatus:    domain.YouTubeCommunityShortsAlarmStateStatusEnqueued,
				}).Error)
			},
			persist: func(t *testing.T, repo batchRepository, ctx context.Context, publishedAt, detectedAt time.Time) error {
				t.Helper()

				post := &domain.YouTubeCommunityPost{
					PostID:        "post-duplicate",
					ChannelID:     "channel-1",
					AuthorName:    "author",
					ContentText:   "hello",
					PublishedText: "1 hour ago",
					PublishedAt:   &publishedAt,
					LikeCount:     10,
					CommentCount:  2,
				}
				return repo.PersistCommunityPosts(ctx, []*domain.YouTubeCommunityPost{post}, []*domain.YouTubeNotificationOutbox{{
					Kind:      domain.OutboxKindCommunityPost,
					ChannelID: "channel-1",
					ContentID: "post-duplicate",
					Payload:   buildCommunityNotificationPayload(post, post.PostID),
					Status:    domain.OutboxStatusPending,
				}}, []*domain.YouTubeContentAlarmTracking{{
					Kind:              domain.OutboxKindCommunityPost,
					ContentID:         "post-duplicate",
					ChannelID:         "channel-1",
					ActualPublishedAt: &publishedAt,
					DetectedAt:        detectedAt.Add(time.Minute),
				}}, nil)
			},
		},
		{
			name:      "short",
			kind:      domain.OutboxKindNewShort,
			postID:    "short:video-duplicate",
			contentID: "video-duplicate",
			seed: func(t *testing.T, db *gorm.DB, publishedAt, detectedAt, authorizedAt time.Time) {
				t.Helper()

				video := &domain.YouTubeVideo{VideoID: "video-duplicate", ChannelID: "channel-1", Title: "title-short-duplicate", IsShort: true, PublishedAt: &publishedAt, ViewCount: 42}
				require.NoError(t, db.Create(&domain.YouTubeNotificationOutbox{
					Kind:          domain.OutboxKindNewShort,
					ChannelID:     "channel-1",
					ContentID:     "video-duplicate",
					Payload:       buildShortNotificationPayload(video, "video-duplicate"),
					Status:        domain.OutboxStatusPending,
					NextAttemptAt: authorizedAt,
					CreatedAt:     authorizedAt,
				}).Error)
				require.NoError(t, db.Create(&domain.YouTubeCommunityShortsAlarmState{
					Kind:              domain.OutboxKindNewShort,
					PostID:            "short:video-duplicate",
					ContentID:         "video-duplicate",
					ChannelID:         "channel-1",
					ActualPublishedAt: &publishedAt,
					DetectedAt:        detectedAt,
					AuthorizedAt:      &authorizedAt,
					DeliveryStatus:    domain.YouTubeCommunityShortsAlarmStateStatusEnqueued,
				}).Error)
			},
			persist: func(t *testing.T, repo batchRepository, ctx context.Context, publishedAt, detectedAt time.Time) error {
				t.Helper()

				video := &domain.YouTubeVideo{VideoID: "video-duplicate", ChannelID: "channel-1", Title: "title-short-duplicate", IsShort: true, PublishedAt: &publishedAt, ViewCount: 42}
				return repo.PersistVideos(ctx, []*domain.YouTubeVideo{video}, []*domain.YouTubeNotificationOutbox{{
					Kind:      domain.OutboxKindNewShort,
					ChannelID: "channel-1",
					ContentID: "short:video-duplicate",
					Payload:   buildShortNotificationPayload(video, "short:video-duplicate"),
					Status:    domain.OutboxStatusPending,
				}}, []*domain.YouTubeContentAlarmTracking{{
					Kind:              domain.OutboxKindNewShort,
					ContentID:         "short:video-duplicate",
					ChannelID:         "channel-1",
					ActualPublishedAt: &publishedAt,
					DetectedAt:        detectedAt.Add(time.Minute),
				}}, nil)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			db := newBatchTestDB(t,
				&domain.YouTubeCommunityPost{},
				&domain.YouTubeVideo{},
				&domain.YouTubeNotificationOutbox{},
				&domain.YouTubeContentWatermark{},
			)
			repo := newBatchRepository(db)
			ctx := context.Background()
			publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
			detectedAt := publishedAt.Add(20 * time.Second)
			authorizedAt := detectedAt.Add(30 * time.Second)

			tc.seed(t, db, publishedAt, detectedAt, authorizedAt)
			require.NoError(t, tc.persist(t, repo, ctx, publishedAt, detectedAt))

			var outbox domain.YouTubeNotificationOutbox
			require.NoError(t, db.First(&outbox, "kind = ? AND content_id = ?", tc.kind, tc.contentID).Error)

			var trackingRow domain.YouTubeContentAlarmTracking
			require.NoError(t, db.First(&trackingRow, "kind = ? AND content_id = ?", tc.kind, tc.contentID).Error)
			require.Equal(t, tc.postID, trackingRow.CanonicalContentID)

			var stateRows []domain.YouTubeCommunityShortsAlarmState
			require.NoError(t, db.Where("kind = ?", tc.kind).Find(&stateRows).Error)
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

func TestGormBatchRepositoryPersistCommunityPostsCollectsSourcePostsWithoutTrackingRows(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeContentWatermark{},
	)
	repo := newBatchRepository(db)
	ctx := context.Background()
	publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)

	err := repo.PersistCommunityPosts(ctx, []*domain.YouTubeCommunityPost{{
		PostID:        "post-1",
		ChannelID:     "channel-1",
		AuthorName:    "author",
		ContentText:   "hello",
		PublishedText: "1 hour ago",
		PublishedAt:   &publishedAt,
		LikeCount:     10,
		CommentCount:  2,
	}}, nil, nil, &domain.YouTubeContentWatermark{
		ChannelID:     "channel-1",
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   false,
		LastContentID: "post-1",
	})
	require.NoError(t, err)

	var sourcePost domain.YouTubeCommunityShortsSourcePost
	require.NoError(t, db.First(&sourcePost, "kind = ? AND post_id = ?", domain.OutboxKindCommunityPost, "community:post-1").Error)
	require.Equal(t, "channel-1", sourcePost.ChannelID)
	require.NotNil(t, sourcePost.ActualPublishedAt)
	require.Equal(t, publishedAt, sourcePost.ActualPublishedAt.UTC())
	require.False(t, sourcePost.DetectedAt.IsZero())
}

func TestGormBatchRepositoryPersistCommunityPostsRejectsPublishedAtStorageRuleMismatch(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	repo := newBatchRepository(db)
	ctx := context.Background()
	publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)

	err := persistCommunityPosts(repo, ctx, []*domain.YouTubeCommunityPost{{
		PostID:        "post-1",
		ChannelID:     "channel-1",
		AuthorName:    "author",
		ContentText:   "hello",
		PublishedText: "1 hour ago",
		PublishedAt:   &publishedAt,
		LikeCount:     10,
		CommentCount:  2,
	}}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindCommunityPost,
		ChannelID: "channel-1",
		ContentID: "post-1",
		Payload:   `{"canonical_post_id":"community:post-1","post_id":"post-1","published_at":"2026-04-10T10:11:12+09:00"}`,
		Status:    domain.OutboxStatusPending,
	}}, &domain.YouTubeContentWatermark{
		ChannelID:     "channel-1",
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: "post-1",
	})
	require.ErrorContains(t, err, "payload published_at mismatch")

	var postCount int64
	require.NoError(t, db.Model(&domain.YouTubeCommunityPost{}).Count(&postCount).Error)
	require.Zero(t, postCount)

	var outboxCount int64
	require.NoError(t, db.Model(&domain.YouTubeNotificationOutbox{}).Count(&outboxCount).Error)
	require.Zero(t, outboxCount)
}

func TestGormBatchRepositoryPersistVideosCollectsSourcePostsWithoutTrackingRows(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeContentWatermark{},
	)
	repo := newBatchRepository(db)
	ctx := context.Background()
	publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)

	err := repo.PersistVideos(ctx, []*domain.YouTubeVideo{{
		VideoID:     "short-1",
		ChannelID:   "channel-1",
		Title:       "title-short-1",
		IsShort:     true,
		PublishedAt: &publishedAt,
	}}, nil, nil, &domain.YouTubeContentWatermark{ChannelID: "channel-1", WatermarkType: domain.WatermarkTypeShort, Initialized: false, LastContentID: "short-1"})
	require.NoError(t, err)

	var sourcePost domain.YouTubeCommunityShortsSourcePost
	require.NoError(t, db.First(&sourcePost, "kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:short-1").Error)
	require.Equal(t, "channel-1", sourcePost.ChannelID)
	require.NotNil(t, sourcePost.ActualPublishedAt)
	require.Equal(t, publishedAt, sourcePost.ActualPublishedAt.UTC())
	require.False(t, sourcePost.DetectedAt.IsZero())
}

func TestGormBatchRepositoryPersistVideosRejectsShortCanonicalPostIDMismatch(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	repo := newBatchRepository(db)
	ctx := context.Background()
	publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)

	err := persistVideos(repo, ctx, []*domain.YouTubeVideo{{
		VideoID:     "short-1",
		ChannelID:   "channel-1",
		Title:       "title-short-1",
		IsShort:     true,
		PublishedAt: &publishedAt,
	}}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindNewShort,
		ChannelID: "channel-1",
		ContentID: "short-1",
		Payload:   `{"canonical_post_id":"short:short-other","video_id":"short-1","published_at":"2026-04-10T01:11:12Z"}`,
		Status:    domain.OutboxStatusPending,
	}}, &domain.YouTubeContentWatermark{ChannelID: "channel-1", WatermarkType: domain.WatermarkTypeShort, Initialized: true, LastContentID: "short-1"})
	require.ErrorContains(t, err, "payload canonical_post_id mismatch")
}

func TestGormBatchRepositoryPersistVideosReusesLegacyRawShortIdentity(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	repo := newBatchRepository(db)
	ctx := context.Background()
	publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	shortVideo := &domain.YouTubeVideo{
		VideoID:     "short-1",
		ChannelID:   "channel-1",
		Title:       "title-short-1",
		IsShort:     true,
		PublishedAt: &publishedAt,
	}

	require.NoError(t, db.Create(&domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewShort,
		ChannelID:     "channel-1",
		ContentID:     "short-1",
		Payload:       buildShortNotificationPayload(shortVideo, "short-1"),
		Status:        domain.OutboxStatusPending,
		NextAttemptAt: publishedAt,
		CreatedAt:     publishedAt,
	}).Error)
	require.NoError(t, db.Create(&domain.YouTubeContentAlarmTracking{
		Kind:               domain.OutboxKindNewShort,
		ContentID:          "short-1",
		CanonicalContentID: "short:short-1",
		ChannelID:          "channel-1",
		ActualPublishedAt:  &publishedAt,
		DetectedAt:         publishedAt,
	}).Error)

	err := repo.PersistVideos(ctx, []*domain.YouTubeVideo{shortVideo}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindNewShort,
		ChannelID: "channel-1",
		ContentID: "short:short-1",
		Payload:   buildShortNotificationPayload(shortVideo, "short:short-1"),
		Status:    domain.OutboxStatusPending,
	}}, []*domain.YouTubeContentAlarmTracking{{
		Kind:              domain.OutboxKindNewShort,
		ContentID:         "short:short-1",
		ChannelID:         "channel-1",
		ActualPublishedAt: &publishedAt,
		DetectedAt:        publishedAt.Add(time.Minute),
	}}, &domain.YouTubeContentWatermark{
		ChannelID:     "channel-1",
		WatermarkType: domain.WatermarkTypeShort,
		Initialized:   true,
		LastContentID: "short:short-1",
	})
	require.NoError(t, err)

	var outboxRows []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	require.Equal(t, "short-1", outboxRows[0].ContentID)

	var trackingRows []domain.YouTubeContentAlarmTracking
	require.NoError(t, db.Order("content_id ASC").Find(&trackingRows).Error)
	require.Len(t, trackingRows, 1)
	require.Equal(t, "short-1", trackingRows[0].ContentID)

	var watermark domain.YouTubeContentWatermark
	require.NoError(t, db.Where("channel_id = ? AND watermark_type = ?", "channel-1", domain.WatermarkTypeShort).First(&watermark).Error)
	require.Equal(t, "short:short-1", watermark.LastContentID)
}

func TestGormBatchRepositoryPersistCommunityPostsRejectsCanonicalPostIDMismatch(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	repo := newBatchRepository(db)
	ctx := context.Background()
	publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)

	err := persistCommunityPosts(repo, ctx, []*domain.YouTubeCommunityPost{{
		PostID:        "post-1",
		ChannelID:     "channel-1",
		AuthorName:    "author",
		ContentText:   "hello",
		PublishedText: "1 hour ago",
		PublishedAt:   &publishedAt,
		LikeCount:     10,
		CommentCount:  2,
	}}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindCommunityPost,
		ChannelID: "channel-1",
		ContentID: "post-1",
		Payload:   `{"canonical_post_id":"community:post-other","post_id":"post-1","published_at":"2026-04-10T01:11:12Z"}`,
		Status:    domain.OutboxStatusPending,
	}}, &domain.YouTubeContentWatermark{
		ChannelID:     "channel-1",
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: "post-1",
	})
	require.ErrorContains(t, err, "payload canonical_post_id mismatch")
}
func TestGormBatchRepositoryPersistCommunityPostsBackfillsPublishedAt(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	repo := newBatchRepository(db)
	ctx := context.Background()

	err := persistCommunityPosts(repo, ctx, []*domain.YouTubeCommunityPost{{
		PostID:        "post-1",
		ChannelID:     "channel-1",
		AuthorName:    "author",
		ContentText:   "hello",
		PublishedText: "1 hour ago",
		LikeCount:     10,
		CommentCount:  2,
	}}, nil, &domain.YouTubeContentWatermark{
		ChannelID:     "channel-1",
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: "post-1",
	})
	require.NoError(t, err)

	publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	err = persistCommunityPosts(repo, ctx, []*domain.YouTubeCommunityPost{{
		PostID:        "post-1",
		ChannelID:     "channel-1",
		AuthorName:    "author",
		ContentText:   "hello",
		PublishedText: "1 hour ago",
		PublishedAt:   &publishedAt,
		LikeCount:     11,
		CommentCount:  3,
	}}, nil, &domain.YouTubeContentWatermark{
		ChannelID:     "channel-1",
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: "post-1",
	})
	require.NoError(t, err)

	var post domain.YouTubeCommunityPost
	require.NoError(t, db.First(&post, "post_id = ?", "post-1").Error)
	require.NotNil(t, post.PublishedAt)
	require.Equal(t, publishedAt, post.PublishedAt.UTC())
	require.EqualValues(t, 11, post.LikeCount)
	require.EqualValues(t, 3, post.CommentCount)
}

func TestGormBatchRepositoryPersistCommunityPostsReactivatesFailedOutboxAndFailedDeliveries(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeNotificationDelivery{},
		&domain.YouTubeContentWatermark{},
	)
	repo := newBatchRepository(db)
	ctx := context.Background()

	publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	createdAt := publishedAt.Add(-15 * time.Minute)
	nextAttemptAt := publishedAt.Add(-2 * time.Minute)
	sentAt := publishedAt.Add(-time.Minute)

	existingOutbox := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindCommunityPost,
		ChannelID:     "channel-1",
		ContentID:     "post-1",
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
		PostID:        "post-1",
		ChannelID:     "channel-1",
		AuthorName:    "author",
		ContentText:   "hello",
		PublishedText: "1 hour ago",
		PublishedAt:   &publishedAt,
		LikeCount:     10,
		CommentCount:  2,
	}

	err := persistCommunityPosts(repo, ctx, []*domain.YouTubeCommunityPost{post}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindCommunityPost,
		ChannelID: "channel-1",
		ContentID: "post-1",
		Payload:   buildCommunityNotificationPayload(post, post.PostID),
		Status:    domain.OutboxStatusPending,
	}}, &domain.YouTubeContentWatermark{
		ChannelID:     "channel-1",
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: "post-1",
	})
	require.NoError(t, err)

	var outboxRows []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	require.Equal(t, existingOutbox.ID, outboxRows[0].ID)
	require.Equal(t, domain.OutboxStatusPending, outboxRows[0].Status)
	require.Zero(t, outboxRows[0].AttemptCount)
	require.True(t, outboxRows[0].NextAttemptAt.After(nextAttemptAt))
	require.Empty(t, outboxRows[0].Error)
	require.Nil(t, outboxRows[0].LockedAt)
	require.Nil(t, outboxRows[0].SentAt)
	require.Contains(t, outboxRows[0].Payload, `"canonical_post_id":"community:post-1"`)

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

func TestGormBatchRepositoryPersistCommunityPostsFinalizesFailedOutboxWhenTrackingAlreadySent(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeNotificationDelivery{},
		&domain.YouTubeContentWatermark{},
	)
	repo := newBatchRepository(db)
	ctx := context.Background()

	publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	detectedAt := publishedAt.Add(20 * time.Second)
	createdAt := publishedAt.Add(-15 * time.Minute)
	nextAttemptAt := publishedAt.Add(-2 * time.Minute)
	sentAt := detectedAt.Add(40 * time.Second)

	existingOutbox := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindCommunityPost,
		ChannelID:     "channel-1",
		ContentID:     "post-1",
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
		ContentID:          "post-1",
		CanonicalContentID: "community:post-1",
		ChannelID:          "channel-1",
		ActualPublishedAt:  &publishedAt,
		DetectedAt:         detectedAt,
		AlarmSentAt:        &sentAt,
		DeliveryStatus:     domain.YouTubeContentAlarmDeliveryStatusSent,
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

	err := persistCommunityPosts(repo, ctx, []*domain.YouTubeCommunityPost{post}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindCommunityPost,
		ChannelID: "channel-1",
		ContentID: "post-1",
		Payload:   buildCommunityNotificationPayload(post, post.PostID),
		Status:    domain.OutboxStatusPending,
	}}, &domain.YouTubeContentWatermark{
		ChannelID:     "channel-1",
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: "post-1",
	})
	require.NoError(t, err)

	var outboxRows []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	require.Equal(t, domain.OutboxStatusSent, outboxRows[0].Status)
	require.NotNil(t, outboxRows[0].SentAt)
	require.Equal(t, sentAt, outboxRows[0].SentAt.UTC())
	require.Empty(t, outboxRows[0].Error)
	require.Equal(t, 3, outboxRows[0].AttemptCount)

	var deliveryRows []domain.YouTubeNotificationDelivery
	require.NoError(t, db.Find(&deliveryRows).Error)
	require.Len(t, deliveryRows, 1)
	require.Equal(t, domain.OutboxStatusSent, deliveryRows[0].Status)
	require.NotNil(t, deliveryRows[0].SentAt)
	require.Equal(t, sentAt, deliveryRows[0].SentAt.UTC())
	require.Empty(t, deliveryRows[0].Error)
	require.Equal(t, 3, deliveryRows[0].AttemptCount)
}

func TestGormBatchRepositoryPersistVideosFinalizesFailedOutboxWhenAlarmStateAlreadySent(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeNotificationDelivery{},
		&domain.YouTubeContentWatermark{},
	)
	repo := newBatchRepository(db)
	ctx := context.Background()

	publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	detectedAt := publishedAt.Add(20 * time.Second)
	createdAt := publishedAt.Add(-15 * time.Minute)
	nextAttemptAt := publishedAt.Add(-2 * time.Minute)
	sentAt := detectedAt.Add(40 * time.Second)
	shortVideo := &domain.YouTubeVideo{
		VideoID:     "video-1",
		ChannelID:   "channel-1",
		Title:       "title-short-1",
		IsShort:     true,
		PublishedAt: &publishedAt,
		ViewCount:   42,
	}

	existingOutbox := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewShort,
		ChannelID:     "channel-1",
		ContentID:     "video-1",
		Payload:       buildShortNotificationPayload(shortVideo, "video-1"),
		Status:        domain.OutboxStatusFailed,
		AttemptCount:  2,
		NextAttemptAt: nextAttemptAt,
		CreatedAt:     createdAt,
		Error:         "old failed",
	}
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
		PostID:            "short:video-1",
		ContentID:         "video-1",
		ChannelID:         "channel-1",
		ActualPublishedAt: &publishedAt,
		DetectedAt:        detectedAt,
		AlarmSentAt:       &sentAt,
		DeliveryStatus:    domain.YouTubeCommunityShortsAlarmStateStatusSent,
	}).Error)

	err := repo.PersistVideos(ctx,
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

	var outboxRows []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	require.Equal(t, domain.OutboxStatusSent, outboxRows[0].Status)
	require.NotNil(t, outboxRows[0].SentAt)
	require.Equal(t, sentAt, outboxRows[0].SentAt.UTC())
	require.Empty(t, outboxRows[0].Error)
	require.Equal(t, 2, outboxRows[0].AttemptCount)

	var deliveryRows []domain.YouTubeNotificationDelivery
	require.NoError(t, db.Find(&deliveryRows).Error)
	require.Len(t, deliveryRows, 1)
	require.Equal(t, domain.OutboxStatusSent, deliveryRows[0].Status)
	require.NotNil(t, deliveryRows[0].SentAt)
	require.Equal(t, sentAt, deliveryRows[0].SentAt.UTC())
	require.Empty(t, deliveryRows[0].Error)
	require.Equal(t, 2, deliveryRows[0].AttemptCount)
}

func TestGormBatchRepositoryPersistCommunityPostsConflictWithSentOutboxBackfillsTrackingSentState(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeNotificationDelivery{},
		&domain.YouTubeContentWatermark{},
	)
	repo := newBatchRepository(db)
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

	err := repo.PersistCommunityPosts(ctx, []*domain.YouTubeCommunityPost{post}, []*domain.YouTubeNotificationOutbox{{
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
	repo := newBatchRepository(db)
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

	err := repo.PersistCommunityPosts(ctx, []*domain.YouTubeCommunityPost{post}, []*domain.YouTubeNotificationOutbox{{
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
	repo := newBatchRepository(db)
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

	err := repo.PersistVideos(ctx,
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
	repo := newBatchRepository(db)
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

	err := persistVideos(repo, ctx, []*domain.YouTubeVideo{{
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
	repo := newBatchRepository(db)
	ctx := context.Background()

	err := persistVideos(repo, ctx, []*domain.YouTubeVideo{{
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
	repo := newBatchRepository(db)
	ctx := context.Background()

	err := persistVideos(repo, ctx, []*domain.YouTubeVideo{{
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

func newBatchTestDB(t *testing.T, models ...any) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqlDB.Close())
	})

	for _, model := range models {
		require.NoError(t, createBatchTestTable(db, model))
	}
	require.NoError(t, createBatchTestTable(db, &domain.YouTubeContentAlarmTracking{}))
	require.NoError(t, createBatchTestTable(db, &domain.YouTubeCommunityShortsSourcePost{}))
	require.NoError(t, createBatchTestTable(db, &domain.YouTubeCommunityShortsAlarmState{}))
	require.NoError(t, createBatchTestTable(db, &domain.YouTubeNotificationDelivery{}))
	require.NoError(t, createBatchTestTable(db, &domain.YouTubeNotificationDeliveryTelemetry{}))

	return db
}

func persistVideos(repo batchRepository, ctx context.Context, videos []*domain.YouTubeVideo, notifications []*domain.YouTubeNotificationOutbox, watermark *domain.YouTubeContentWatermark) error {
	return repo.PersistVideos(ctx, videos, notifications, nil, watermark)
}

func persistCommunityPosts(repo batchRepository, ctx context.Context, posts []*domain.YouTubeCommunityPost, notifications []*domain.YouTubeNotificationOutbox, watermark *domain.YouTubeContentWatermark) error {
	return repo.PersistCommunityPosts(ctx, posts, notifications, nil, watermark)
}

func createBatchTestTable(db *gorm.DB, model any) error {
	switch model.(type) {
	case *domain.YouTubeVideo:
		return db.Exec(`
			CREATE TABLE IF NOT EXISTS youtube_videos (
				video_id TEXT PRIMARY KEY,
				channel_id TEXT NOT NULL,
				title TEXT NOT NULL,
				thumbnail TEXT,
				duration TEXT,
				published_text TEXT,
				published_at DATETIME,
				is_short BOOLEAN NOT NULL DEFAULT FALSE,
				is_live_replay BOOLEAN NOT NULL DEFAULT FALSE,
				view_count INTEGER NOT NULL DEFAULT 0,
				first_seen_at DATETIME NOT NULL,
				last_seen_at DATETIME NOT NULL
			)
		`).Error
	case *domain.YouTubeCommunityPost:
		return db.Exec(`
			CREATE TABLE IF NOT EXISTS youtube_community_posts (
				post_id TEXT PRIMARY KEY,
				channel_id TEXT NOT NULL,
				author_name TEXT,
				author_photo TEXT,
				content_text TEXT,
				published_text TEXT,
				published_at DATETIME,
				like_count INTEGER NOT NULL DEFAULT 0,
				comment_count INTEGER NOT NULL DEFAULT 0,
				images TEXT,
				attached_video TEXT,
				first_seen_at DATETIME NOT NULL,
				last_seen_at DATETIME NOT NULL
			)
		`).Error
	case *domain.YouTubeNotificationOutbox:
		return db.Exec(`
			CREATE TABLE IF NOT EXISTS youtube_notification_outbox (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				kind TEXT NOT NULL,
				channel_id TEXT NOT NULL,
				content_id TEXT NOT NULL,
				payload TEXT NOT NULL,
				status TEXT NOT NULL,
				attempt_count INTEGER NOT NULL DEFAULT 0,
				next_attempt_at DATETIME NOT NULL,
				created_at DATETIME NOT NULL,
				locked_at DATETIME,
				sent_at DATETIME,
				error TEXT,
				UNIQUE(kind, content_id)
			)
		`).Error
	case *domain.YouTubeContentWatermark:
		return db.Exec(`
			CREATE TABLE IF NOT EXISTS youtube_content_watermarks (
				channel_id TEXT NOT NULL,
				watermark_type TEXT NOT NULL,
				initialized BOOLEAN NOT NULL DEFAULT FALSE,
				last_content_id TEXT NOT NULL DEFAULT '',
				updated_at DATETIME NOT NULL,
				PRIMARY KEY (channel_id, watermark_type)
			)
		`).Error
	case *domain.YouTubeContentAlarmTracking:
		return db.Exec(`
			CREATE TABLE IF NOT EXISTS youtube_content_alarm_tracking (
				kind TEXT NOT NULL,
				content_id TEXT NOT NULL,
				canonical_content_id TEXT NOT NULL,
				channel_id TEXT NOT NULL,
				actual_published_at DATETIME,
				detected_at DATETIME NOT NULL,
				alarm_sent_at DATETIME,
				alarm_latency_millis INTEGER,
				alarm_latency_exceeded BOOLEAN,
				delivery_status TEXT NOT NULL DEFAULT 'PENDING',
				latency_classification_status TEXT,
				delay_source TEXT,
				internal_delay_cause TEXT,
				created_at DATETIME NOT NULL,
				updated_at DATETIME NOT NULL,
				PRIMARY KEY (kind, content_id),
				UNIQUE(kind, canonical_content_id)
			)
		`).Error
	case *domain.YouTubeCommunityShortsSourcePost:
		return db.Exec(
			`
			CREATE TABLE IF NOT EXISTS youtube_community_shorts_source_posts (
				kind TEXT NOT NULL,
				post_id TEXT NOT NULL,
				channel_id TEXT NOT NULL,
				actual_published_at DATETIME,
				detected_at DATETIME NOT NULL,
				created_at DATETIME NOT NULL,
				updated_at DATETIME NOT NULL,
				PRIMARY KEY (kind, post_id)
			)
			`).Error
	case *domain.YouTubeCommunityShortsAlarmState:
		return db.Exec(
			`
			CREATE TABLE IF NOT EXISTS youtube_community_shorts_alarm_states (
				kind TEXT NOT NULL,
				post_id TEXT NOT NULL,
				content_id TEXT NOT NULL,
				channel_id TEXT NOT NULL,
				actual_published_at DATETIME,
				detected_at DATETIME NOT NULL,
				published_at_retry_after DATETIME,
				authorized_at DATETIME,
				alarm_sent_at DATETIME,
				delivery_status TEXT NOT NULL DEFAULT 'DETECTED',
				created_at DATETIME NOT NULL,
				updated_at DATETIME NOT NULL,
				PRIMARY KEY (kind, post_id),
				UNIQUE(kind, content_id)
			)
			`).Error
	case *domain.YouTubeNotificationDeliveryTelemetry:
		return db.Exec(`
			CREATE TABLE IF NOT EXISTS youtube_notification_delivery_telemetry (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				delivery_id INTEGER NOT NULL,
				attempt_ordinal INTEGER NOT NULL,
				outbox_id INTEGER NOT NULL,
				channel_id TEXT NOT NULL,
				content_id TEXT NOT NULL,
				post_id TEXT NOT NULL,
				room_id TEXT NOT NULL,
				alarm_type TEXT NOT NULL,
				actual_published_at DATETIME,
				alarm_sent_at DATETIME,
				alarm_latency_millis INTEGER,
				detected_at DATETIME,
				observation_status TEXT NOT NULL DEFAULT 'unclassified',
				observation_runtime_name TEXT,
				observation_bigbang_cutover_at DATETIME,
				observation_started_at DATETIME,
				observation_ended_at DATETIME,
				dedupe_key TEXT NOT NULL,
				delivery_path TEXT NOT NULL,
				delivery_mode TEXT NOT NULL,
				send_result TEXT NOT NULL,
				failure_reason TEXT,
				attempt_started_at DATETIME,
				attempt_finished_at DATETIME,
				event_at DATETIME NOT NULL,
				next_attempt_at DATETIME NOT NULL,
				created_at DATETIME,
				locked_at DATETIME,
				logged_at DATETIME,
				error TEXT,
				UNIQUE(delivery_id, attempt_ordinal)
			)
		`).Error
	case *domain.YouTubeNotificationDelivery:
		return db.Exec(`
			CREATE TABLE IF NOT EXISTS youtube_notification_delivery (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				outbox_id INTEGER NOT NULL,
				room_id TEXT NOT NULL,
				status TEXT NOT NULL,
				attempt_count INTEGER NOT NULL DEFAULT 0,
				next_attempt_at DATETIME NOT NULL,
				created_at DATETIME NOT NULL,
				locked_at DATETIME,
				sent_at DATETIME,
				error TEXT,
				UNIQUE(outbox_id, room_id)
			)
		`).Error
	default:
		return fmt.Errorf("unsupported test model: %T", model)
	}
}
