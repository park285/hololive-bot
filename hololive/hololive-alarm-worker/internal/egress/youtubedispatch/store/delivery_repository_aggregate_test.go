package store

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func seedAggregateOutbox(ctx context.Context, t *testing.T, db *pgxpool.Pool, contentID string, outboxStatus domain.OutboxStatus, deliveryStatuses []domain.OutboxStatus) int64 {
	t.Helper()

	now := time.Now().UTC().Truncate(time.Microsecond)

	var outboxID int64

	err := db.QueryRow(ctx, `
		INSERT INTO youtube_notification_outbox
			(kind, channel_id, content_id, payload, status, attempt_count, next_attempt_at, created_at, locked_at)
		VALUES ($1, $2, $3, '{}'::jsonb, $4, 0, $5, $5, $5)
		RETURNING id
	`, string(domain.OutboxKindNewVideo), "channel-agg", contentID, string(outboxStatus), now).Scan(&outboxID)
	require.NoError(t, err)

	for i, status := range deliveryStatuses {
		_, err := db.Exec(ctx, `
			INSERT INTO youtube_notification_delivery
				(outbox_id, room_id, status, attempt_count, next_attempt_at, created_at)
			VALUES ($1, $2, $3, 0, $4, $4)
		`, outboxID, "room-agg-"+string(rune('a'+i)), string(status), now)
		require.NoError(t, err)
	}

	return outboxID
}

func readOutboxAggregateRow(ctx context.Context, t *testing.T, db *pgxpool.Pool, outboxID int64) (outboxStatus domain.OutboxStatus, sentAtValue, lockedAtValue *time.Time, errorText string) {
	t.Helper()

	var (
		status           domain.OutboxStatus
		sentAt, lockedAt *time.Time
		errText          string
	)

	err := db.QueryRow(ctx, `
		SELECT status, sent_at, locked_at, COALESCE(error, '')
		FROM youtube_notification_outbox
		WHERE id = $1
	`, outboxID).Scan(&status, &sentAt, &lockedAt, &errText)
	require.NoError(t, err)

	return status, sentAt, lockedAt, errText
}

func readOutboxTerminalAt(ctx context.Context, t *testing.T, db *pgxpool.Pool, outboxID int64) *time.Time {
	t.Helper()

	var terminalAt *time.Time

	require.NoError(t, db.QueryRow(ctx,
		"SELECT terminal_at FROM youtube_notification_outbox WHERE id = $1", outboxID).Scan(&terminalAt))

	return terminalAt
}

func TestUpdateOutboxAggregateStatuses_AllSentMarksSentOnce(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repository := NewDeliveryRepository(pool, slog.New(slog.DiscardHandler))
	outboxID := seedAggregateOutbox(ctx, t, pool, "agg-sent", domain.OutboxStatusPending,
		[]domain.OutboxStatus{domain.OutboxStatusSent, domain.OutboxStatusSent})

	require.NoError(t, repository.UpdateOutboxAggregateStatuses(ctx, []int64{outboxID}))

	status, sentAt, lockedAt, errText := readOutboxAggregateRow(ctx, t, pool, outboxID)
	require.Equal(t, domain.OutboxStatusSent, status)
	require.NotNil(t, sentAt)
	require.Nil(t, lockedAt)
	require.Empty(t, errText)

	terminalAt := readOutboxTerminalAt(ctx, t, pool, outboxID)
	require.NotNil(t, terminalAt)

	require.NoError(t, repository.UpdateOutboxAggregateStatuses(ctx, []int64{outboxID}))
	require.Equal(t, terminalAt, readOutboxTerminalAt(ctx, t, pool, outboxID))
}

func TestUpdateOutboxAggregateStatuses_DoesNotRewriteExistingSentAt(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repository := NewDeliveryRepository(pool, slog.New(slog.DiscardHandler))
	outboxID := seedAggregateOutbox(ctx, t, pool, "agg-sent-keep", domain.OutboxStatusSent,
		[]domain.OutboxStatus{domain.OutboxStatusSent})

	firstSentAt := time.Date(2026, time.July, 1, 10, 0, 0, 0, time.UTC)
	_, err := pool.Exec(ctx,
		"UPDATE youtube_notification_outbox SET sent_at = $1 WHERE id = $2", firstSentAt, outboxID)
	require.NoError(t, err)

	require.NoError(t, repository.UpdateOutboxAggregateStatuses(ctx, []int64{outboxID}))

	_, sentAt, _, _ := readOutboxAggregateRow(ctx, t, pool, outboxID)
	require.NotNil(t, sentAt)
	require.True(t, sentAt.UTC().Equal(firstSentAt),
		"reconcile/aggregate-sync 재실행이 sent_at을 재기록하면 latency 통계가 오염된다: got %s", sentAt)
}

func TestUpdateOutboxAggregateStatuses_FailedWinsOverSent(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repository := NewDeliveryRepository(pool, slog.New(slog.DiscardHandler))
	outboxID := seedAggregateOutbox(ctx, t, pool, "agg-failed", domain.OutboxStatusPending,
		[]domain.OutboxStatus{domain.OutboxStatusSent, domain.OutboxStatusFailed})

	require.NoError(t, repository.UpdateOutboxAggregateStatuses(ctx, []int64{outboxID}))

	status, sentAt, _, errText := readOutboxAggregateRow(ctx, t, pool, outboxID)
	require.Equal(t, domain.OutboxStatusFailed, status)
	require.Nil(t, sentAt)
	require.Equal(t, "per-room delivery failed", errText)
}

func TestUpdateOutboxAggregateStatuses_PendingDeliveryKeepsOutboxPending(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repository := NewDeliveryRepository(pool, slog.New(slog.DiscardHandler))
	outboxID := seedAggregateOutbox(ctx, t, pool, "agg-pending", domain.OutboxStatusSent,
		[]domain.OutboxStatus{domain.OutboxStatusSent, domain.OutboxStatusPending})

	require.NoError(t, repository.UpdateOutboxAggregateStatuses(ctx, []int64{outboxID}))

	status, _, _, _ := readOutboxAggregateRow(ctx, t, pool, outboxID)
	require.Equal(t, domain.OutboxStatusPending, status)
	require.Nil(t, readOutboxTerminalAt(ctx, t, pool, outboxID))
}

func TestUpdateOutboxAggregateStatuses_TerminalTransitionRefreshesTerminalAt(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repository := NewDeliveryRepository(pool, slog.New(slog.DiscardHandler))
	outboxID := seedAggregateOutbox(ctx, t, pool, "agg-terminal-transition", domain.OutboxStatusSent,
		[]domain.OutboxStatus{domain.OutboxStatusFailed})
	oldTerminalAt := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

	_, err := pool.Exec(ctx,
		"UPDATE youtube_notification_outbox SET terminal_at = $1 WHERE id = $2", oldTerminalAt, outboxID)
	require.NoError(t, err)
	require.NoError(t, repository.UpdateOutboxAggregateStatuses(ctx, []int64{outboxID}))

	status, _, _, _ := readOutboxAggregateRow(ctx, t, pool, outboxID)
	require.Equal(t, domain.OutboxStatusFailed, status)

	terminalAt := readOutboxTerminalAt(ctx, t, pool, outboxID)
	require.NotNil(t, terminalAt)
	require.True(t, terminalAt.After(oldTerminalAt))
}

func TestUpdateOutboxAggregateStatuses_BackfillsMissingTerminalAtWithoutStatusChange(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repository := NewDeliveryRepository(pool, slog.New(slog.DiscardHandler))
	outboxID := seedAggregateOutbox(ctx, t, pool, "agg-terminal-at-missing", domain.OutboxStatusFailed,
		[]domain.OutboxStatus{domain.OutboxStatusFailed})

	require.Nil(t, readOutboxTerminalAt(ctx, t, pool, outboxID))
	require.NoError(t, repository.UpdateOutboxAggregateStatuses(ctx, []int64{outboxID}))

	status, _, _, _ := readOutboxAggregateRow(ctx, t, pool, outboxID)
	require.Equal(t, domain.OutboxStatusFailed, status)
	require.NotNil(t, readOutboxTerminalAt(ctx, t, pool, outboxID))
}

func TestUpdateOutboxAggregateStatuses_NoDeliveriesDefaultsToPending(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repository := NewDeliveryRepository(pool, slog.New(slog.DiscardHandler))
	outboxID := seedAggregateOutbox(ctx, t, pool, "agg-empty", domain.OutboxStatusSent, nil)

	require.NoError(t, repository.UpdateOutboxAggregateStatuses(ctx, []int64{outboxID}))

	status, _, _, _ := readOutboxAggregateRow(ctx, t, pool, outboxID)
	require.Equal(t, domain.OutboxStatusPending, status)
}

func TestUpdateOutboxAggregateStatuses_UnchangedStatusIsNoOpWrite(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repository := NewDeliveryRepository(pool, slog.New(slog.DiscardHandler))
	outboxID := seedAggregateOutbox(ctx, t, pool, "agg-noop", domain.OutboxStatusPending,
		[]domain.OutboxStatus{domain.OutboxStatusPending})

	var xminBefore uint32

	require.NoError(t, pool.QueryRow(ctx,
		"SELECT xmin FROM youtube_notification_outbox WHERE id = $1", outboxID).Scan(&xminBefore))

	require.NoError(t, repository.UpdateOutboxAggregateStatuses(ctx, []int64{outboxID}))

	var xminAfter uint32

	require.NoError(t, pool.QueryRow(ctx,
		"SELECT xmin FROM youtube_notification_outbox WHERE id = $1", outboxID).Scan(&xminAfter))
	require.Equal(t, xminBefore, xminAfter,
		"IS DISTINCT FROM 가드: 상태가 같으면 새 row version(dead tuple)을 만들지 않아야 한다")
}

// legacyAggregateSyncCandidateSQL은 EXISTS/NOT EXISTS 전환 이전의 GROUP BY/HAVING 후보 선택을
// 동등성 기준으로 보존합니다. 후보 집합, 오름차순, LIMIT 경계가 같아야 합니다.
const legacyAggregateSyncCandidateSQL = `
	SELECT d.outbox_id
	FROM youtube_notification_delivery d
	JOIN youtube_notification_outbox o ON o.id = d.outbox_id
	WHERE o.status = $1
	GROUP BY d.outbox_id
	HAVING SUM(CASE WHEN d.status IN ($2, $3) THEN 1 ELSE 0 END) = 0
	ORDER BY d.outbox_id ASC
	LIMIT $4`

func TestFindPendingOutboxIDsForAggregateSync_MatchesGroupedCandidateSet(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repository := NewDeliveryRepository(pool, slog.New(slog.DiscardHandler))

	allSent := seedAggregateOutbox(ctx, t, pool, "agg-sync-all-sent", domain.OutboxStatusPending,
		[]domain.OutboxStatus{domain.OutboxStatusSent, domain.OutboxStatusSent})
	seedAggregateOutbox(ctx, t, pool, "agg-sync-pending-child", domain.OutboxStatusPending,
		[]domain.OutboxStatus{domain.OutboxStatusSent, domain.OutboxStatusPending})

	failedAndQuarantined := seedAggregateOutbox(ctx, t, pool, "agg-sync-failed-quarantined", domain.OutboxStatusPending,
		[]domain.OutboxStatus{domain.OutboxStatusFailed, DeliveryStatusQuarantined})
	seedAggregateOutbox(ctx, t, pool, "agg-sync-no-child", domain.OutboxStatusPending, nil)
	seedAggregateOutbox(ctx, t, pool, "agg-sync-sent-outbox", domain.OutboxStatusSent,
		[]domain.OutboxStatus{domain.OutboxStatusSent})
	seedAggregateOutbox(ctx, t, pool, "agg-sync-sending-child", domain.OutboxStatusPending,
		[]domain.OutboxStatus{domain.OutboxStatusFailed, DeliveryStatusSending})

	allFailed := seedAggregateOutbox(ctx, t, pool, "agg-sync-all-failed", domain.OutboxStatusPending,
		[]domain.OutboxStatus{domain.OutboxStatusFailed})

	want := []int64{allSent, failedAndQuarantined, allFailed}

	for batchSize := 1; batchSize <= len(want)+1; batchSize++ {
		got, err := repository.FindPendingOutboxIDsForAggregateSync(ctx, batchSize)
		require.NoError(t, err)
		require.Equal(t, want[:min(batchSize, len(want))], got, "batch size %d", batchSize)

		rows, err := pool.Query(ctx, legacyAggregateSyncCandidateSQL,
			domain.OutboxStatusPending, domain.OutboxStatusPending, DeliveryStatusSending, batchSize)
		require.NoError(t, err)

		legacy, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		require.NoError(t, err)
		require.Equal(t, legacy, got, "batch size %d must match the grouped candidate set", batchSize)
	}
}
