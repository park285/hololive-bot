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

package batchrepo

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

func TestPgxBatchRepositoryPersistVideos(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	repository := NewBatchRepository(db)
	ctx := context.Background()

	videos := make([]*domain.YouTubeVideo, 0, PollerBatchMaxSize+5)
	notifications := make([]*domain.YouTubeNotificationOutbox, 0, PollerBatchMaxSize+5)
	for i := range PollerBatchMaxSize + 5 {
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

	err := persistVideos(repository, ctx, videos, notifications, &domain.YouTubeContentWatermark{
		ChannelID:     "channel-1",
		WatermarkType: domain.WatermarkTypeVideo,
		Initialized:   true,
		LastContentID: "video-054",
	})
	require.NoError(t, err)

	var videoCount int64
	require.NoError(t, db.Model(&domain.YouTubeVideo{}).Count(&videoCount).Error)
	require.EqualValues(t, PollerBatchMaxSize+5, videoCount)

	var outboxCount int64
	require.NoError(t, db.Model(&domain.YouTubeNotificationOutbox{}).Count(&outboxCount).Error)
	require.EqualValues(t, PollerBatchMaxSize+5, outboxCount)

	var watermark domain.YouTubeContentWatermark
	require.NoError(t, db.Where("channel_id = ? AND watermark_type = ?", "channel-1", domain.WatermarkTypeVideo).First(&watermark).Error)
	require.Equal(t, "video-054", watermark.LastContentID)

	err = persistVideos(repository, ctx, []*domain.YouTubeVideo{{
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
	require.EqualValues(t, PollerBatchMaxSize+5, outboxCount)
}

func TestPgxBatchRepositoryPersistVideosAllowsDifferentKindsForSameContentID(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	repository := NewBatchRepository(db)
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

	err := persistVideos(repository, ctx, []*domain.YouTubeVideo{{
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

func TestPgxBatchRepositoryPersistVideosConcurrentOutboxInsertIsIdempotent(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	require.NoError(t, db.Exec("PRAGMA busy_timeout = 5000").Error)
	ctx := context.Background()

	runPersist := func() error {
		repository := NewBatchRepository(db)
		return persistVideos(repository, ctx, []*domain.YouTubeVideo{{
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

func TestPgxBatchRepositoryPersistVideosPrimaryAndBackfillSameContentLeaveOneOutboxRow(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	repository := NewBatchRepository(db)
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

	require.NoError(t, persistVideos(repository, ctx, []*domain.YouTubeVideo{video}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindNewShort,
		ChannelID: "channel-1",
		ContentID: "short:short-backfill",
		Payload:   buildShortNotificationPayload(video, "short:short-backfill"),
		Status:    domain.OutboxStatusPending,
	}}, watermark))
	require.NoError(t, persistVideos(repository, ctx, []*domain.YouTubeVideo{video}, []*domain.YouTubeNotificationOutbox{{
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

func TestPgxBatchRepositoryPersistVideosPersistsShortPublishedAt(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	repository := NewBatchRepository(db)
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

	err := persistVideos(repository, ctx, []*domain.YouTubeVideo{shortVideo}, []*domain.YouTubeNotificationOutbox{{
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
	require.Equal(t, yttimestamp.Format(canonicalPublishedAt), stored.PublishedAt.UTC().Format(time.RFC3339Nano))

	var outbox domain.YouTubeNotificationOutbox
	require.NoError(t, db.First(&outbox, "kind = ? AND content_id = ?", domain.OutboxKindNewShort, "short:short-1").Error)
	require.Contains(t, outbox.Payload, `"canonical_post_id": "short:short-1"`)
	require.Contains(t, outbox.Payload, `"published_at": "`+yttimestamp.Format(canonicalPublishedAt)+`"`)

	var watermark domain.YouTubeContentWatermark
	require.NoError(t, db.Where("channel_id = ? AND watermark_type = ?", "channel-1", domain.WatermarkTypeShort).First(&watermark).Error)
	require.Equal(t, "short:short-1", watermark.LastContentID)
}

func TestPgxBatchRepositoryPersistVideosPreservesExistingPublishedAt(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	repository := NewBatchRepository(db)
	ctx := context.Background()
	firstPublishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	laterPublishedAt := firstPublishedAt.Add(5 * time.Minute)

	require.NoError(t, persistVideos(repository, ctx, []*domain.YouTubeVideo{{
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
	require.NoError(t, persistVideos(repository, ctx, []*domain.YouTubeVideo{{
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

func TestPgxBatchRepositoryPersistVideosRejectsShortPublishedAtStorageRuleMismatch(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	repository := NewBatchRepository(db)
	ctx := context.Background()
	publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)

	err := persistVideos(repository, ctx, []*domain.YouTubeVideo{{
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

func TestPgxBatchRepositoryPersistCommunityPosts(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	repository := NewBatchRepository(db)
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

	err := persistCommunityPosts(repository, ctx, []*domain.YouTubeCommunityPost{communityPost}, []*domain.YouTubeNotificationOutbox{{
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
	require.Contains(t, outbox.Payload, `"canonical_post_id": "community:post-1"`)
	require.Contains(t, outbox.Payload, `"published_at": "`+publishedAt.Format(time.RFC3339Nano)+`"`)

	var watermark domain.YouTubeContentWatermark
	require.NoError(t, db.Where("channel_id = ? AND watermark_type = ?", "channel-1", domain.WatermarkTypeCommunityPost).First(&watermark).Error)
	require.Equal(t, "post-1", watermark.LastContentID)
}

func TestPgxBatchRepositoryPersistCommunityPostsPreservesExistingPublishedAt(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	repository := NewBatchRepository(db)
	ctx := context.Background()
	firstPublishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	laterPublishedAt := firstPublishedAt.Add(5 * time.Minute)

	require.NoError(t, persistCommunityPosts(repository, ctx, []*domain.YouTubeCommunityPost{{
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
	require.NoError(t, persistCommunityPosts(repository, ctx, []*domain.YouTubeCommunityPost{{
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

func TestPgxBatchRepositoryPersistCommunityPostsUpsertsAlarmState(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	repository := NewBatchRepository(db)
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

	err := repository.PersistCommunityPosts(ctx, []*domain.YouTubeCommunityPost{post}, []*domain.YouTubeNotificationOutbox{{
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

func TestPgxBatchRepositoryPersistCommunityShortsDuplicatePollKeepsExistingClaimState(t *testing.T) {
	testCases := []struct {
		name      string
		kind      domain.OutboxKind
		postID    string
		contentID string
		seed      func(t *testing.T, db *batchTestDB, publishedAt, detectedAt, authorizedAt time.Time)
		persist   func(t *testing.T, repository BatchRepository, ctx context.Context, publishedAt, detectedAt time.Time) error
	}{
		{
			name:      "community post",
			kind:      domain.OutboxKindCommunityPost,
			postID:    "community:post-duplicate",
			contentID: "post-duplicate",
			seed: func(t *testing.T, db *batchTestDB, publishedAt, detectedAt, authorizedAt time.Time) {
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
			persist: func(t *testing.T, repository BatchRepository, ctx context.Context, publishedAt, detectedAt time.Time) error {
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
				return repository.PersistCommunityPosts(ctx, []*domain.YouTubeCommunityPost{post}, []*domain.YouTubeNotificationOutbox{{
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
			seed: func(t *testing.T, db *batchTestDB, publishedAt, detectedAt, authorizedAt time.Time) {
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
			persist: func(t *testing.T, repository BatchRepository, ctx context.Context, publishedAt, detectedAt time.Time) error {
				t.Helper()

				video := &domain.YouTubeVideo{VideoID: "video-duplicate", ChannelID: "channel-1", Title: "title-short-duplicate", IsShort: true, PublishedAt: &publishedAt, ViewCount: 42}
				return repository.PersistVideos(ctx, []*domain.YouTubeVideo{video}, []*domain.YouTubeNotificationOutbox{{
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
			repository := NewBatchRepository(db)
			ctx := context.Background()
			publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
			detectedAt := publishedAt.Add(20 * time.Second)
			authorizedAt := detectedAt.Add(30 * time.Second)

			tc.seed(t, db, publishedAt, detectedAt, authorizedAt)
			require.NoError(t, tc.persist(t, repository, ctx, publishedAt, detectedAt))

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
