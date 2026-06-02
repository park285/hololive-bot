package batchrepo

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type spyLatencyPersister struct {
	called     bool
	identities []LatencyClassificationIdentity
}

func (s *spyLatencyPersister) PersistPostLatencyClassificationsByIdentities(ctx context.Context, identities []LatencyClassificationIdentity) error {
	s.called = true
	s.identities = append(s.identities, identities...)
	return nil
}

func TestGormBatchRepositoryPersistVideosCallsLatencyPersister(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	spy := &spyLatencyPersister{}
	repository := NewGormBatchRepositoryWithPersister(db, spy)
	ctx := context.Background()

	publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	detectedAt := publishedAt.Add(20 * time.Second)
	shortVideo := &domain.YouTubeVideo{
		VideoID:     "video-latency-1",
		ChannelID:   "channel-1",
		Title:       "title-short-latency",
		IsShort:     true,
		PublishedAt: &publishedAt,
		ViewCount:   42,
	}

	err := repository.PersistVideos(ctx,
		[]*domain.YouTubeVideo{shortVideo},
		[]*domain.YouTubeNotificationOutbox{{
			Kind:      domain.OutboxKindNewShort,
			ChannelID: "channel-1",
			ContentID: "short:video-latency-1",
			Payload:   buildShortNotificationPayload(shortVideo, "short:video-latency-1"),
			Status:    domain.OutboxStatusPending,
		}},
		[]*domain.YouTubeContentAlarmTracking{{
			Kind:              domain.OutboxKindNewShort,
			ContentID:         "short:video-latency-1",
			ChannelID:         "channel-1",
			ActualPublishedAt: &publishedAt,
			DetectedAt:        detectedAt,
		}},
		&domain.YouTubeContentWatermark{
			ChannelID:     "channel-1",
			WatermarkType: domain.WatermarkTypeShort,
			Initialized:   true,
			LastContentID: "short:video-latency-1",
		},
	)
	require.NoError(t, err)

	require.True(t, spy.called, "latency persister must be called after commit")
	require.Len(t, spy.identities, 1)
	require.Equal(t, domain.OutboxKindNewShort, spy.identities[0].Kind)
	require.Equal(t, "short:video-latency-1", spy.identities[0].ContentID)
}

func TestGormBatchRepositoryPersistCommunityPostsCallsLatencyPersister(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	spy := &spyLatencyPersister{}
	repository := NewGormBatchRepositoryWithPersister(db, spy)
	ctx := context.Background()

	publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	detectedAt := publishedAt.Add(20 * time.Second)
	post := &domain.YouTubeCommunityPost{
		PostID:        "post-latency-1",
		ChannelID:     "channel-1",
		AuthorName:    "author",
		ContentText:   "hello",
		PublishedText: "1 hour ago",
		PublishedAt:   &publishedAt,
		LikeCount:     10,
		CommentCount:  2,
	}

	err := repository.PersistCommunityPosts(ctx,
		[]*domain.YouTubeCommunityPost{post},
		[]*domain.YouTubeNotificationOutbox{{
			Kind:      domain.OutboxKindCommunityPost,
			ChannelID: "channel-1",
			ContentID: "post-latency-1",
			Payload:   buildCommunityNotificationPayload(post, post.PostID),
			Status:    domain.OutboxStatusPending,
		}},
		[]*domain.YouTubeContentAlarmTracking{{
			Kind:               domain.OutboxKindCommunityPost,
			ContentID:          "post-latency-1",
			CanonicalContentID: "community:post-latency-1",
			ChannelID:          "channel-1",
			ActualPublishedAt:  &publishedAt,
			DetectedAt:         detectedAt,
		}},
		nil,
	)
	require.NoError(t, err)

	require.True(t, spy.called, "latency persister must be called after commit")
	require.Len(t, spy.identities, 1)
	require.Equal(t, domain.OutboxKindCommunityPost, spy.identities[0].Kind)
	require.Equal(t, "post-latency-1", spy.identities[0].ContentID)
}
