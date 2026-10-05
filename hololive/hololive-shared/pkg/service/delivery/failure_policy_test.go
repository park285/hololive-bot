package delivery

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestDeliveryFailureUsesClaimedAttemptAndPreservesTerminalSchedule(t *testing.T) {
	repo := testRepository(t)
	ctx := t.Context()
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "attempt-policy", "room", "message"))

	items := fetchAndLockItems(ctx, t, repo)
	require.Len(t, items, 1)

	id := items[0].ID

	// 소유자가 같아도 오래된 claim 횟수로 다른 시도를 확정하지 않습니다.
	ok, err := repo.MarkFailed(ctx, id, testWorkerA, 2, 3, time.Hour, "stale count")
	require.NoError(t, err)
	require.False(t, ok)

	var nextAttemptAt time.Time

	err = repo.pool.QueryRow(ctx, `UPDATE notification_delivery_outbox SET attempt_count=2 WHERE id=$1 RETURNING next_attempt_at`, id).Scan(&nextAttemptAt)
	require.NoError(t, err)

	ok, err = repo.MarkFailed(ctx, id, testWorkerA, 2, 3, time.Hour, "exhausted")
	require.NoError(t, err)
	require.True(t, ok)

	var (
		status    string
		attempts  int
		scheduled time.Time
	)

	require.NoError(t, repo.pool.QueryRow(ctx, `SELECT status,attempt_count,next_attempt_at FROM notification_delivery_outbox WHERE id=$1`, id).Scan(&status, &attempts, &scheduled))
	require.Equal(t, "FAILED", status)
	require.Equal(t, 3, attempts)
	require.Equal(t, nextAttemptAt, scheduled)
}

func TestDeliveryReissuePreservesRequestOnStaleAttemptAndExhaustsGeneration(t *testing.T) {
	repo := testRepository(t)
	ctx := t.Context()
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "reissue-policy", "room", "message"))

	items := fetchAndLockItems(ctx, t, repo)
	require.Len(t, items, 1)

	item := &items[0]
	previous := &preparedMessage{Body: "immutable request body", Route: testPreparedText, BaseID: notificationDeliveryClientRequestID(item)}
	ok, err := repo.saveRequest(ctx, item.ID, testWorkerA, nil, previous)
	require.NoError(t, err)
	require.True(t, ok)
	markOutboxSending(ctx, t, repo, item)

	next := *previous

	next.Exhausted = true

	ok, err = repo.reissueFailedRequest(ctx, item.ID, testWorkerA, 1, previous, &next, 3, time.Hour, "rejected")
	require.NoError(t, err)
	require.False(t, ok)

	ok, err = repo.reissueFailedRequest(ctx, item.ID, testWorkerA, item.AttemptCount, previous, &next, 3, time.Hour, "rejected")
	require.NoError(t, err)
	require.True(t, ok)

	var (
		status, reason string
		attempts       int
		scheduled      time.Time
	)

	require.NoError(t, repo.pool.QueryRow(ctx, `SELECT status,attempt_count,next_attempt_at,error FROM notification_delivery_outbox WHERE id=$1`, item.ID).Scan(&status, &attempts, &scheduled, &reason))
	require.Equal(t, "FAILED", status)
	require.Equal(t, 1, attempts)
	require.Equal(t, item.NextAttemptAt, scheduled)
	require.Equal(t, "client request ID generations exhausted: rejected", reason)
}
