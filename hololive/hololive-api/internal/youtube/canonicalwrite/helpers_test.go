package canonicalwrite

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"testing"
	"time"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/contracts/youtubeoutbox"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	testChannelID  = "channel-1"
	testPostID     = "post-1"
	testVideoID    = "video-1"
	testShortID    = "short-1"
	testShortTitle = "title-short-1"

	testCanonicalPostID           = "community:" + testPostID
	testCanonicalShortFromShortID = "short:" + testShortID
	testCanonicalShortFromVideoID = "short:" + testVideoID

	testDuplicatePostID  = "post-duplicate"
	testDuplicateVideoID = "video-duplicate"
	// NEW_SHORT canonical content_id(testDuplicateVideoID 기준).
	testDuplicateShortContentID = "short:video-duplicate"

	testAuthorName    = "author"
	testContentText   = "hello"
	testPublishedText = "1 hour ago"

	outboxColumns     = "id, kind, channel_id, content_id, payload, status, attempt_count, next_attempt_at, created_at, locked_at, sent_at, COALESCE(error, '') AS error"
	trackingColumns   = "kind, content_id, canonical_content_id, channel_id, actual_published_at, detected_at, alarm_sent_at, alarm_latency_millis, alarm_latency_exceeded, delivery_status, COALESCE(latency_classification_status, '') AS latency_classification_status, COALESCE(delay_source, '') AS delay_source, COALESCE(internal_delay_cause, '') AS internal_delay_cause, created_at, updated_at"
	videoColumns      = "video_id, published_at, view_count"
	postColumns       = "post_id, published_at, like_count, comment_count"
	watermarkColumns  = "channel_id, watermark_type, last_content_id"
	sourcePostColumns = "kind, post_id, channel_id, actual_published_at, detected_at"
	alarmStateColumns = "kind, post_id, content_id, channel_id, actual_published_at, detected_at, authorized_at, alarm_sent_at, delivery_status"
)

// commitVideos는 source observation Finalize처럼 트랜잭션 하나에서 PersistVideosTx를 실행하고, 오류면 롤백한다.
func commitVideos(
	ctx context.Context,
	pool *pgxpool.Pool,
	videos []*domain.YouTubeVideo,
	notifications []*domain.YouTubeNotificationOutbox,
	trackingRows []*domain.YouTubeContentAlarmTracking,
	watermark *domain.YouTubeContentWatermark,
) error {
	if err := dbx.InPgxTx(ctx, pool, func(tx dbx.Tx) error {
		return PersistVideosTx(ctx, tx, videos, notifications, trackingRows, watermark)
	}); err != nil {
		return fmt.Errorf("commit videos: %w", err)
	}

	return nil
}

// commitCommunityPosts는 source observation Finalize처럼 트랜잭션 하나에서 PersistCommunityPostsTx를 실행하고, 오류면 롤백한다.
func commitCommunityPosts(
	ctx context.Context,
	pool *pgxpool.Pool,
	posts []*domain.YouTubeCommunityPost,
	notifications []*domain.YouTubeNotificationOutbox,
	trackingRows []*domain.YouTubeContentAlarmTracking,
	watermark *domain.YouTubeContentWatermark,
) error {
	if err := dbx.InPgxTx(ctx, pool, func(tx dbx.Tx) error {
		return PersistCommunityPostsTx(ctx, tx, posts, notifications, trackingRows, watermark)
	}); err != nil {
		return fmt.Errorf("commit community posts: %w", err)
	}

	return nil
}

// shortPayload는 운영 producer와 같은 youtubeoutbox.Short 계약으로 payload를 만든다.
func shortPayload(t *testing.T, video *domain.YouTubeVideo, canonicalPostID string) string {
	t.Helper()

	data, err := jsonv2.Marshal(youtubeoutbox.Short{
		VideoFields:     youtubeoutbox.NewVideoFields(video),
		CanonicalPostID: canonicalPostID,
	})
	require.NoError(t, err)

	return string(data)
}

// communityPayload는 운영 producer와 같은 youtubeoutbox.Community 계약으로 payload를 만든다.
func communityPayload(t *testing.T, post *domain.YouTubeCommunityPost, resourceID, canonicalPostID string) string {
	t.Helper()

	payloadPost := *post

	payloadPost.PostID = resourceID

	data, err := jsonv2.Marshal(youtubeoutbox.NewCommunity(&payloadPost, canonicalPostID))
	require.NoError(t, err)

	return string(data)
}

func countRows(t *testing.T, pool *pgxpool.Pool, query string, args ...any) int64 {
	t.Helper()

	var count int64

	require.NoError(t, pool.QueryRow(t.Context(), query, args...).Scan(&count))

	return count
}

func getRow[T any](t *testing.T, pool *pgxpool.Pool, query string, args ...any) T {
	t.Helper()

	var row T

	require.NoError(t, pgxscan.Get(t.Context(), pool, &row, query, args...))

	return row
}

func selectRows[T any](t *testing.T, pool *pgxpool.Pool, query string, args ...any) []T {
	t.Helper()

	var rows []T

	require.NoError(t, pgxscan.Select(t.Context(), pool, &rows, query, args...))

	return rows
}

func seedOutbox(t *testing.T, pool *pgxpool.Pool, row *domain.YouTubeNotificationOutbox) int64 {
	t.Helper()

	var id int64

	require.NoError(t, pool.QueryRow(t.Context(), `
		INSERT INTO youtube_notification_outbox
			(kind, channel_id, content_id, payload, status, attempt_count, next_attempt_at, created_at, sent_at, error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id`,
		row.Kind, row.ChannelID, row.ContentID, row.Payload, row.Status, row.AttemptCount, row.NextAttemptAt, row.CreatedAt, row.SentAt, row.Error,
	).Scan(&id))

	return id
}

func seedDelivery(t *testing.T, pool *pgxpool.Pool, row *domain.YouTubeNotificationDelivery) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
		INSERT INTO youtube_notification_delivery
			(outbox_id, room_id, status, attempt_count, next_attempt_at, created_at, sent_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		row.OutboxID, row.RoomID, row.Status, row.AttemptCount, row.NextAttemptAt, row.CreatedAt, row.SentAt)
	require.NoError(t, err)
}

func seedAlarmState(t *testing.T, pool *pgxpool.Pool, row *domain.YouTubeCommunityShortsAlarmState) {
	t.Helper()

	now := time.Now()

	_, err := pool.Exec(t.Context(), `
		INSERT INTO youtube_community_shorts_alarm_states
			(kind, post_id, content_id, channel_id, actual_published_at, detected_at, authorized_at, alarm_sent_at, delivery_status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		row.Kind, row.PostID, row.ContentID, row.ChannelID, row.ActualPublishedAt, row.DetectedAt, row.AuthorizedAt, row.AlarmSentAt, row.DeliveryStatus, now, now)
	require.NoError(t, err)
}
