package store

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/service/youtube/tracking/observation"
)

func TestMarkSendingBatchIfLockedRejectsStaleRelockWithinOneMillisecond(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repository := NewDeliveryRepository(pool, slog.New(slog.DiscardHandler))
	staleLockedAt := time.Date(2026, time.June, 3, 12, 0, 0, 123456000, time.UTC)
	currentLockedAt := staleLockedAt.Add(500 * time.Microsecond)
	deliveryID := seedLockedDelivery(ctx, t, pool, staleLockedAt)

	_, err := pool.Exec(ctx, `
		UPDATE youtube_notification_delivery
		SET locked_at = $1
		WHERE id = $2
	`, currentLockedAt, deliveryID)
	require.NoError(t, err)

	sendingRows, err := repository.MarkSendingBatchIfLocked(ctx, []LockToken{NewLockToken(deliveryID, &staleLockedAt)})
	require.NoError(t, err)
	require.Empty(t, sendingRows)

	status, lockedAt, sentAt := readDeliveryStatusAndLocks(ctx, t, pool, deliveryID)
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

	status, lockedAt, sentAt = readDeliveryStatusAndLocks(ctx, t, pool, deliveryID)
	require.Equal(t, domain.OutboxStatusSent, status)
	require.Nil(t, lockedAt)
	require.NotNil(t, sentAt)
	requireDeliveryLedgerStatus(ctx, t, pool, domain.OutboxKindNewVideo, "video-lock-race", "room-lock-race", LedgerStatusSent)
}

const (
	lockStateContentID = "post-lock-state"
	lockStateChannelID = "channel-lock-state"
	lockStateClaimID   = "community:post-lock-state"
)

func lockStateClaimToken(authorizedAt time.Time) dispatchstate.ClaimToken {
	return dispatchstate.ClaimToken{
		Kind:         domain.OutboxKindCommunityPost,
		PostID:       lockStateClaimID,
		AuthorizedAt: authorizedAt,
	}
}

func seedLockStateTracking(ctx context.Context, t *testing.T, repository *observation.PgxRepository, publishedAt, detectedAt, authorizedAt time.Time) {
	t.Helper()

	require.NoError(t, repository.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:              domain.OutboxKindCommunityPost,
		ContentID:         lockStateContentID,
		ChannelID:         lockStateChannelID,
		ActualPublishedAt: &publishedAt,
		DetectedAt:        detectedAt,
	}))
	require.NoError(t, repository.UpsertAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:              domain.OutboxKindCommunityPost,
		PostID:            lockStateContentID,
		ContentID:         lockStateContentID,
		ChannelID:         lockStateChannelID,
		ActualPublishedAt: &publishedAt,
		DetectedAt:        detectedAt,
		AuthorizedAt:      &authorizedAt,
	}))
}

func requireLockStateTrackingUnsent(ctx context.Context, t *testing.T, repository *observation.PgxRepository) {
	t.Helper()

	trackingRow, err := repository.FindByIdentity(ctx, domain.OutboxKindCommunityPost, lockStateContentID)
	require.NoError(t, err)
	require.NotNil(t, trackingRow)
	require.Nil(t, trackingRow.AlarmSentAt)
}

func requireLockStateTrackingSent(ctx context.Context, t *testing.T, repository *observation.PgxRepository) {
	t.Helper()

	trackingRow, err := repository.FindByIdentity(ctx, domain.OutboxKindCommunityPost, lockStateContentID)
	require.NoError(t, err)
	require.NotNil(t, trackingRow)
	require.NotNil(t, trackingRow.AlarmSentAt)
	require.Equal(t, domain.YouTubeContentAlarmDeliveryStatusSent, trackingRow.DeliveryStatus)

	stateRow, err := repository.FindAlarmStateByPostID(ctx, domain.OutboxKindCommunityPost, lockStateContentID)
	require.NoError(t, err)
	require.NotNil(t, stateRow)
	require.Nil(t, stateRow.AuthorizedAt)
	require.NotNil(t, stateRow.AlarmSentAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusSent, stateRow.DeliveryStatus)
}

func TestMarkSentBatchIfLockedPersistsTrackingAfterSendingGate(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repository := NewDeliveryRepository(pool, slog.New(slog.DiscardHandler))
	trackingRepository := observation.NewRepositoryContext(t.Context(), pool)
	staleLockedAt := time.Date(2026, time.June, 3, 12, 0, 0, 123456000, time.UTC)
	currentLockedAt := staleLockedAt.Add(500 * time.Microsecond)
	authorizedAt := staleLockedAt.Add(10 * time.Second)
	deliveryID := seedLockedCommunityDelivery(ctx, t, pool, staleLockedAt)

	seedLockStateTracking(ctx, t, trackingRepository, staleLockedAt.Add(-2*time.Minute), staleLockedAt.Add(-time.Minute), authorizedAt)

	_, err := pool.Exec(ctx, `
		UPDATE youtube_notification_delivery
		SET locked_at = $1
		WHERE id = $2
	`, currentLockedAt, deliveryID)
	require.NoError(t, err)

	staleTokens := []LockToken{NewLockToken(deliveryID, &staleLockedAt)}
	require.NoError(t, repository.MarkSentBatchIfLocked(ctx, staleTokens, lockStateClaimToken(authorizedAt)))

	status, lockedAt, sentAt := readDeliveryStatusAndLocks(ctx, t, pool, deliveryID)
	require.Equal(t, domain.OutboxStatusPending, status)
	require.NotNil(t, lockedAt)
	require.True(t, lockedAt.Equal(currentLockedAt), "locked_at = %s, want %s", lockedAt, currentLockedAt)
	require.Nil(t, sentAt)
	requireLockStateTrackingUnsent(ctx, t, trackingRepository)

	sendingRows, err := repository.MarkSendingBatchIfLocked(ctx, []LockToken{NewLockToken(deliveryID, &currentLockedAt)})
	require.NoError(t, err)
	require.Len(t, sendingRows, 1)

	sentTokens := DeliveryLockTokensForIDs(sendingRows, []int64{deliveryID})
	require.NoError(t, repository.MarkSentBatchIfLocked(ctx, sentTokens, lockStateClaimToken(authorizedAt)))

	status, lockedAt, sentAt = readDeliveryStatusAndLocks(ctx, t, pool, deliveryID)
	require.Equal(t, domain.OutboxStatusSent, status)
	require.Nil(t, lockedAt)
	require.NotNil(t, sentAt)
	requireLockStateTrackingSent(ctx, t, trackingRepository)
}

func TestFetchAndLockDoesNotReclaimSendingRows(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repository := NewDeliveryRepository(pool, slog.New(slog.DiscardHandler))
	lockedAt := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Microsecond)
	deliveryID := seedLockedDelivery(ctx, t, pool, lockedAt)

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

func TestLegacyMarkFailedMethodsDoNotOverwriteSentRows(t *testing.T) {
	methods := map[string]func(context.Context, *DeliveryRepository, int64) error{
		"single": func(ctx context.Context, repository *DeliveryRepository, id int64) error {
			return repository.MarkFailed(ctx, id, 3, time.Minute, "stale failure")
		},
		"batch": func(ctx context.Context, repository *DeliveryRepository, id int64) error {
			return repository.MarkFailedRetryBatch(ctx, []int64{id}, 3, time.Minute, "stale failure")
		},
	}

	for name, markFailed := range methods {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			pool := dbtest.NewPool(t)
			repository := NewDeliveryRepository(pool, slog.New(slog.DiscardHandler))
			deliveryID := seedLockedDelivery(ctx, t, pool, time.Now().UTC().Add(-time.Minute))
			sentAt := time.Now().UTC().Truncate(time.Microsecond)

			_, err := pool.Exec(ctx, `
				UPDATE youtube_notification_delivery
				SET status = $1, sent_at = $2, locked_at = NULL
				WHERE id = $3
			`, domain.OutboxStatusSent, sentAt, deliveryID)
			require.NoError(t, err)
			require.NoError(t, markFailed(ctx, repository, deliveryID))

			status, lockedAt, persistedSentAt := readDeliveryStatusAndLocks(ctx, t, pool, deliveryID)
			require.Equal(t, domain.OutboxStatusSent, status)
			require.Nil(t, lockedAt)
			require.NotNil(t, persistedSentAt)
			require.True(t, persistedSentAt.Equal(sentAt), "sent_at = %s, want %s", persistedSentAt, sentAt)
		})
	}
}

func TestQuarantineStaleSendingMarksTerminalAndAggregateFailed(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repository := NewDeliveryRepository(pool, slog.New(slog.DiscardHandler))
	lockedAt := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Microsecond)
	deliveryID := seedLockedDelivery(ctx, t, pool, lockedAt)
	outboxID := readDeliveryOutboxID(ctx, t, pool, deliveryID)

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

	status, lockedAtAfter, sentAt := readDeliveryStatusAndLocks(ctx, t, pool, deliveryID)
	require.Equal(t, DeliveryStatusQuarantined, status)
	require.Nil(t, lockedAtAfter)
	require.Nil(t, sentAt)

	var (
		attemptCount int
		errMsg       string
	)

	err = pool.QueryRow(ctx, `
		SELECT attempt_count, COALESCE(error, '')
		FROM youtube_notification_delivery
		WHERE id = $1
	`, deliveryID).Scan(&attemptCount, &errMsg)
	require.NoError(t, err)
	require.Equal(t, 1, attemptCount)
	require.Equal(t, "stale sending; external send outcome unknown", errMsg)
	requireDeliveryLedgerStatus(ctx, t, pool, domain.OutboxKindNewVideo, "video-lock-race", "room-lock-race", LedgerStatusQuarantined)

	require.NoError(t, repository.UpdateOutboxAggregateStatuses(ctx, outboxIDs))

	outboxStatus := readOutboxStatus(ctx, t, pool, outboxID)
	require.Equal(t, domain.OutboxStatusFailed, outboxStatus)
}

func requireDeliveryLedgerStatus(
	ctx context.Context,
	t *testing.T,
	db *pgxpool.Pool,
	kind domain.OutboxKind,
	logicalID string,
	roomID string,
	want LedgerStatus,
) {
	t.Helper()

	var got LedgerStatus

	require.NoError(t, db.QueryRow(ctx, `
		SELECT status
		FROM youtube_notification_delivery_ledger
		WHERE kind = $1 AND logical_id = $2 AND room_id = $3
	`, kind, logicalID, roomID).Scan(&got))
	require.Equal(t, want, got)
}

func seedLockedDelivery(ctx context.Context, t *testing.T, db *pgxpool.Pool, lockedAt time.Time) int64 {
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

func seedLockedCommunityDelivery(ctx context.Context, t *testing.T, db *pgxpool.Pool, lockedAt time.Time) int64 {
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
	ctx context.Context,
	t *testing.T,
	db *pgxpool.Pool,
	deliveryID int64,
) (result1 domain.OutboxStatus, result2, result3 *time.Time) {
	t.Helper()

	var (
		status   domain.OutboxStatus
		lockedAt *time.Time
		sentAt   *time.Time
	)

	err := db.QueryRow(ctx, `
		SELECT status, locked_at, sent_at
		FROM youtube_notification_delivery
		WHERE id = $1
	`, deliveryID).Scan(&status, &lockedAt, &sentAt)
	require.NoError(t, err)

	return status, lockedAt, sentAt
}

func readDeliveryOutboxID(ctx context.Context, t *testing.T, db *pgxpool.Pool, deliveryID int64) int64 {
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

func readOutboxStatus(ctx context.Context, t *testing.T, db *pgxpool.Pool, outboxID int64) domain.OutboxStatus {
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
