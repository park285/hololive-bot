package poller

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/kapu/hololive-shared/pkg/domain"
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

	err := repo.PersistVideos(ctx, videos, notifications, &domain.YouTubeContentWatermark{
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

	err = repo.PersistVideos(ctx, []*domain.YouTubeVideo{{
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
			Payload:   `{"video_id":"video-1","kind":"short"}`,
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

	err := repo.PersistVideos(ctx, []*domain.YouTubeVideo{{
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
}

func TestGormBatchRepositoryPersistCommunityPosts(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeCommunityPost{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	repo := newBatchRepository(db)
	ctx := context.Background()

	err := repo.PersistCommunityPosts(ctx, []*domain.YouTubeCommunityPost{{
		PostID:        "post-1",
		ChannelID:     "channel-1",
		AuthorName:    "author",
		ContentText:   "hello",
		PublishedText: "1 hour ago",
		LikeCount:     10,
		CommentCount:  2,
	}}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindCommunityPost,
		ChannelID: "channel-1",
		ContentID: "post-1",
		Payload:   `{"post_id":"post-1"}`,
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

	var watermark domain.YouTubeContentWatermark
	require.NoError(t, db.Where("channel_id = ? AND watermark_type = ?", "channel-1", domain.WatermarkTypeCommunityPost).First(&watermark).Error)
	require.Equal(t, "post-1", watermark.LastContentID)
}

func TestGormBatchRepositoryPersistVideosRollsBackOnNotificationError(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeContentWatermark{},
	)
	repo := newBatchRepository(db)
	ctx := context.Background()

	err := repo.PersistVideos(ctx, []*domain.YouTubeVideo{{
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

func newBatchTestDB(t *testing.T, models ...any) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	for _, model := range models {
		require.NoError(t, createBatchTestTable(db, model))
	}

	return db
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
	default:
		return fmt.Errorf("unsupported test model: %T", model)
	}
}
