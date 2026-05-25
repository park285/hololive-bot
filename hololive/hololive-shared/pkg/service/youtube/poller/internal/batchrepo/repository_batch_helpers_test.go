package batchrepo

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

func persistVideos(repository BatchRepository, ctx context.Context, videos []*domain.YouTubeVideo, notifications []*domain.YouTubeNotificationOutbox, watermark *domain.YouTubeContentWatermark) error {
	return repository.PersistVideos(ctx, videos, notifications, nil, watermark)
}

func persistCommunityPosts(repository BatchRepository, ctx context.Context, posts []*domain.YouTubeCommunityPost, notifications []*domain.YouTubeNotificationOutbox, watermark *domain.YouTubeContentWatermark) error {
	return repository.PersistCommunityPosts(ctx, posts, notifications, nil, watermark)
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
