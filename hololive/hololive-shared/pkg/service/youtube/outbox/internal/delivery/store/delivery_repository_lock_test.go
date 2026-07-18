package store

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatchstate"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

func TestMarkSendingBatchIfLockedRejectsStaleRelockWithinOneMillisecond(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repository := NewDeliveryRepository(pool, slog.New(slog.DiscardHandler))
	staleLockedAt := time.Date(2026, time.June, 3, 12, 0, 0, 123456000, time.UTC)
	currentLockedAt := staleLockedAt.Add(500 * time.Microsecond)
	deliveryID := seedLockedDelivery(t, ctx, pool, staleLockedAt)

	_, err := pool.Exec(ctx, `
		UPDATE youtube_notification_delivery
		SET locked_at = $1
		WHERE id = $2
	`, currentLockedAt, deliveryID)
	require.NoError(t, err)

	sendingRows, err := repository.MarkSendingBatchIfLocked(ctx, []LockToken{NewLockToken(deliveryID, &staleLockedAt)})
	require.NoError(t, err)
	require.Empty(t, sendingRows)

	status, lockedAt, sentAt := readDeliveryStatusAndLocks(t, ctx, pool, deliveryID)
	require.Equal(t, domain.OutboxStatusPending, status)
	require.NotNil(t, lockedAt)
	require.True(t, lockedAt.Equal(currentLockedAt), "locked_at = %s, want %s", lockedAt, currentLockedAt)
	require.Nil(t, sentAt)

	sendingRows, err = repository.MarkSendingBatchIfLocked(ctx, []LockToken{NewLockToken(deliveryID, &currentLockedAt)})
	require.NoError(t, err)
	require.Len(t, sendingRows, 1)
	require.Equal(t, DeliveryStatusSending, sendingRows[0].Status)
	require.NotNil(t, sendingRows[0].LockedAt)

	err = repository.MarkSentBatchIfLocked(ctx, DeliveryLockTokensForIDs(sendingRows, []int64{deliveryID}))
	require.NoError(t, err)

	status, lockedAt, sentAt = readDeliveryStatusAndLocks(t, ctx, pool, deliveryID)
	require.Equal(t, domain.OutboxStatusSent, status)
	require.Nil(t, lockedAt)
	require.NotNil(t, sentAt)
}

func TestMarkSentBatchIfLockedPersistsTrackingAfterSendingGate(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repository := NewDeliveryRepository(pool, slog.New(slog.DiscardHandler))
	trackingRepository := trackingrepo.NewRepository(pool)
	staleLockedAt := time.Date(2026, time.June, 3, 12, 0, 0, 123456000, time.UTC)
	currentLockedAt := staleLockedAt.Add(500 * time.Microsecond)
	authorizedAt := staleLockedAt.Add(10 * time.Second)
	actualPublishedAt := staleLockedAt.Add(-2 * time.Minute)
	detectedAt := staleLockedAt.Add(-time.Minute)
	deliveryID := seedLockedCommunityDelivery(t, ctx, pool, staleLockedAt)

	require.NoError(t, trackingRepository.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:              domain.OutboxKindCommunityPost,
		ContentID:         "post-lock-state",
		ChannelID:         "channel-lock-state",
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
	}))
	require.NoError(t, trackingRepository.UpsertAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:              domain.OutboxKindCommunityPost,
		PostID:            "post-lock-state",
		ContentID:         "post-lock-state",
		ChannelID:         "channel-lock-state",
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
		AuthorizedAt:      &authorizedAt,
	}))
	_, err := pool.Exec(ctx, `
		UPDATE youtube_notification_delivery
		SET locked_at = $1
		WHERE id = $2
	`, currentLockedAt, deliveryID)
	require.NoError(t, err)

	err = repository.MarkSentBatchIfLocked(ctx, []LockToken{NewLockToken(deliveryID, &staleLockedAt)}, dispatchstate.ClaimToken{
		Kind:         domain.OutboxKindCommunityPost,
		PostID:       "community:post-lock-state",
		AuthorizedAt: authorizedAt,
	})
	require.NoError(t, err)

	status, lockedAt, sentAt := readDeliveryStatusAndLocks(t, ctx, pool, deliveryID)
	require.Equal(t, domain.OutboxStatusPending, status)
	require.NotNil(t, lockedAt)
	require.True(t, lockedAt.Equal(currentLockedAt), "locked_at = %s, want %s", lockedAt, currentLockedAt)
	require.Nil(t, sentAt)

	trackingRow, err := trackingRepository.FindByIdentity(ctx, domain.OutboxKindCommunityPost, "post-lock-state")
	require.NoError(t, err)
	require.NotNil(t, trackingRow)
	require.Nil(t, trackingRow.AlarmSentAt)

	sendingRows, err := repository.MarkSendingBatchIfLocked(ctx, []LockToken{NewLockToken(deliveryID, &currentLockedAt)})
	require.NoError(t, err)
	require.Len(t, sendingRows, 1)

	err = repository.MarkSentBatchIfLocked(ctx, DeliveryLockTokensForIDs(sendingRows, []int64{deliveryID}), dispatchstate.ClaimToken{
		Kind:         domain.OutboxKindCommunityPost,
		PostID:       "community:post-lock-state",
		AuthorizedAt: authorizedAt,
	})
	require.NoError(t, err)

	status, lockedAt, sentAt = readDeliveryStatusAndLocks(t, ctx, pool, deliveryID)
	require.Equal(t, domain.OutboxStatusSent, status)
	require.Nil(t, lockedAt)
	require.NotNil(t, sentAt)

	trackingRow, err = trackingRepository.FindByIdentity(ctx, domain.OutboxKindCommunityPost, "post-lock-state")
	require.NoError(t, err)
	require.NotNil(t, trackingRow)
	require.NotNil(t, trackingRow.AlarmSentAt)
	require.Equal(t, domain.YouTubeContentAlarmDeliveryStatusSent, trackingRow.DeliveryStatus)

	stateRow, err := trackingRepository.FindAlarmStateByPostID(ctx, domain.OutboxKindCommunityPost, "post-lock-state")
	require.NoError(t, err)
	require.NotNil(t, stateRow)
	require.Nil(t, stateRow.AuthorizedAt)
	require.NotNil(t, stateRow.AlarmSentAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusSent, stateRow.DeliveryStatus)
}

func TestFetchAndLockDoesNotReclaimSendingRows(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repository := NewDeliveryRepository(pool, slog.New(slog.DiscardHandler))
	lockedAt := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Microsecond)
	deliveryID := seedLockedDelivery(t, ctx, pool, lockedAt)

	_, err := pool.Exec(ctx, `
		UPDATE youtube_notification_delivery
		SET status = $1
		WHERE id = $2
	`, DeliveryStatusSending, deliveryID)
	require.NoError(t, err)

	rows, err := repository.FetchAndLock(ctx, 10, time.Minute)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestQuarantineStaleSendingMarksTerminalAndAggregateFailed(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repository := NewDeliveryRepository(pool, slog.New(slog.DiscardHandler))
	lockedAt := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Microsecond)
	deliveryID := seedLockedDelivery(t, ctx, pool, lockedAt)
	outboxID := readDeliveryOutboxID(t, ctx, pool, deliveryID)

	_, err := pool.Exec(ctx, `
		UPDATE youtube_notification_delivery
		SET status = $1
		WHERE id = $2
	`, DeliveryStatusSending, deliveryID)
	require.NoError(t, err)

	outboxIDs, quarantined, err := repository.QuarantineStaleSending(ctx, time.Minute, 10)
	require.NoError(t, err)
	require.Equal(t, 1, quarantined)
	require.Equal(t, []int64{outboxID}, outboxIDs)

	status, lockedAtAfter, sentAt := readDeliveryStatusAndLocks(t, ctx, pool, deliveryID)
	require.Equal(t, DeliveryStatusQuarantined, status)
	require.Nil(t, lockedAtAfter)
	require.Nil(t, sentAt)

	var attemptCount int
	var errMsg string
	err = pool.QueryRow(ctx, `
		SELECT attempt_count, COALESCE(error, '')
		FROM youtube_notification_delivery
		WHERE id = $1
	`, deliveryID).Scan(&attemptCount, &errMsg)
	require.NoError(t, err)
	require.Equal(t, 1, attemptCount)
	require.Equal(t, "stale sending; external send outcome unknown", errMsg)

	require.NoError(t, repository.UpdateOutboxAggregateStatuses(ctx, outboxIDs))
	outboxStatus := readOutboxStatus(t, ctx, pool, outboxID)
	require.Equal(t, domain.OutboxStatusFailed, outboxStatus)
}

func seedLockedDelivery(t *testing.T, ctx context.Context, db *pgxpool.Pool, lockedAt time.Time) int64 {
	t.Helper()

	var outboxID int64
	err := db.QueryRow(ctx, `
		INSERT INTO youtube_notification_outbox
			(kind, channel_id, content_id, payload, status, attempt_count, next_attempt_at, created_at)
		VALUES ($1, $2, $3, $4::jsonb, $5, 0, $6, $7)
		RETURNING id
	`, string(domain.OutboxKindNewVideo), "channel-lock-race", "video-lock-race", "{}", string(domain.OutboxStatusPending), lockedAt, lockedAt).Scan(&outboxID)
	require.NoError(t, err)

	var deliveryID int64
	err = db.QueryRow(ctx, `
		INSERT INTO youtube_notification_delivery
			(outbox_id, room_id, status, attempt_count, next_attempt_at, created_at, locked_at)
		VALUES ($1, $2, $3, 0, $4, $5, $6)
		RETURNING id
	`, outboxID, "room-lock-race", string(domain.OutboxStatusPending), lockedAt, lockedAt, lockedAt).Scan(&deliveryID)
	require.NoError(t, err)

	return deliveryID
}

func seedLockedCommunityDelivery(t *testing.T, ctx context.Context, db *pgxpool.Pool, lockedAt time.Time) int64 {
	t.Helper()

	var outboxID int64
	err := db.QueryRow(ctx, `
		INSERT INTO youtube_notification_outbox
			(kind, channel_id, content_id, payload, status, attempt_count, next_attempt_at, created_at)
		VALUES ($1, $2, $3, $4::jsonb, $5, 0, $6, $7)
		RETURNING id
	`, string(domain.OutboxKindCommunityPost), "channel-lock-state", "post-lock-state", `{"canonical_post_id":"community:post-lock-state","post_id":"post-lock-state"}`, string(domain.OutboxStatusPending), lockedAt, lockedAt).Scan(&outboxID)
	require.NoError(t, err)

	var deliveryID int64
	err = db.QueryRow(ctx, `
		INSERT INTO youtube_notification_delivery
			(outbox_id, room_id, status, attempt_count, next_attempt_at, created_at, locked_at)
		VALUES ($1, $2, $3, 0, $4, $5, $6)
		RETURNING id
	`, outboxID, "room-lock-state", string(domain.OutboxStatusPending), lockedAt, lockedAt, lockedAt).Scan(&deliveryID)
	require.NoError(t, err)

	return deliveryID
}

func readDeliveryStatusAndLocks(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	deliveryID int64,
) (result1 domain.OutboxStatus, result2, result3 *time.Time) {
	t.Helper()

	var status domain.OutboxStatus
	var lockedAt *time.Time
	var sentAt *time.Time
	err := db.QueryRow(ctx, `
		SELECT status, locked_at, sent_at
		FROM youtube_notification_delivery
		WHERE id = $1
	`, deliveryID).Scan(&status, &lockedAt, &sentAt)
	require.NoError(t, err)
	return status, lockedAt, sentAt
}

func readDeliveryOutboxID(t *testing.T, ctx context.Context, db *pgxpool.Pool, deliveryID int64) int64 {
	t.Helper()

	var outboxID int64
	err := db.QueryRow(ctx, `
		SELECT outbox_id
		FROM youtube_notification_delivery
		WHERE id = $1
	`, deliveryID).Scan(&outboxID)
	require.NoError(t, err)
	return outboxID
}

func readOutboxStatus(t *testing.T, ctx context.Context, db *pgxpool.Pool, outboxID int64) domain.OutboxStatus {
	t.Helper()

	var status domain.OutboxStatus
	err := db.QueryRow(ctx, `
		SELECT status
		FROM youtube_notification_outbox
		WHERE id = $1
	`, outboxID).Scan(&status)
	require.NoError(t, err)
	return status
}
