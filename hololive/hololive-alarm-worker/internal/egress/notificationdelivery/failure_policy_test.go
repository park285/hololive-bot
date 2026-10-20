package notificationdelivery

import (
	jsonv2 "encoding/json/v2"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	deliverypayload "github.com/kapu/hololive-shared/pkg/contracts/notificationdelivery"
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
	requireFenceMiss(t, repo.MarkFailed(ctx, id, testWorkerA, 2, 3, time.Hour, "stale count"), actionMarkFailed, sendfence.ReasonVersionChanged)

	var nextAttemptAt time.Time

	err := repo.pool.QueryRow(ctx, `UPDATE notification_delivery_outbox SET attempt_count=2 WHERE id=$1 RETURNING next_attempt_at`, id).Scan(&nextAttemptAt)
	require.NoError(t, err)

	require.NoError(t, repo.MarkFailed(ctx, id, testWorkerA, 2, 3, time.Hour, "exhausted"))

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

// attempt_count가 bigint이므로 int4 상한에 도달한 claim도 거부하지 않고 종료 상태로 정산합니다.
func TestDeliveryFailureSettlesAttemptCountBeyondInt4Range(t *testing.T) {
	repo := testRepository(t)
	ctx := t.Context()
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "attempt-int4-limit", "room", "message"))

	items := fetchAndLockItems(ctx, t, repo)
	require.Len(t, items, 1)

	id := items[0].ID

	_, err := repo.pool.Exec(ctx, `UPDATE notification_delivery_outbox SET attempt_count=$2 WHERE id=$1`, id, math.MaxInt32)
	require.NoError(t, err)

	require.NoError(t, repo.MarkFailed(ctx, id, testWorkerA, math.MaxInt32, 3, time.Hour, "int4 boundary"))

	var (
		status   string
		attempts int64
	)

	require.NoError(t, repo.pool.QueryRow(ctx, `SELECT status,attempt_count FROM notification_delivery_outbox WHERE id=$1`, id).Scan(&status, &attempts))
	require.Equal(t, "FAILED", status)
	require.Equal(t, int64(math.MaxInt32)+1, attempts)
}

func TestDeliveryReissuePreservesRequestOnStaleAttemptAndExhaustsGeneration(t *testing.T) {
	repo := testRepository(t)
	ctx := t.Context()
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "reissue-policy", "room", "message"))

	items := fetchAndLockItems(ctx, t, repo)
	require.Len(t, items, 1)

	item := &items[0]
	previous := &deliverypayload.Request{Body: "immutable request body", Route: testPreparedText, BaseID: notificationDeliveryClientRequestID(item)}
	require.NoError(t, repo.saveRequest(ctx, item.ID, testWorkerA, item.AttemptCount, previous))
	markOutboxSending(ctx, t, repo, item)

	requireFenceMiss(t, repo.reissueFailedRequest(ctx, item.ID, testWorkerA, 1, 0, true, 3, time.Hour, "rejected"), actionReissueRequest, sendfence.ReasonVersionChanged)
	require.NoError(t, repo.reissueFailedRequest(ctx, item.ID, testWorkerA, item.AttemptCount, 0, true, 3, time.Hour, "rejected"))

	var (
		status, reason string
		attempts       int
		scheduled      time.Time
		raw            []byte
	)

	require.NoError(t, repo.pool.QueryRow(ctx, `SELECT status,attempt_count,next_attempt_at,error,payload->'request' FROM notification_delivery_outbox WHERE id=$1`, item.ID).Scan(&status, &attempts, &scheduled, &reason, &raw))
	require.Equal(t, "FAILED", status)
	require.Equal(t, 1, attempts)
	require.Equal(t, item.NextAttemptAt, scheduled)
	require.Equal(t, "client request ID generations exhausted: rejected", reason)

	var stored deliverypayload.Request

	require.NoError(t, jsonv2.Unmarshal(raw, &stored))

	exhausted := *previous

	exhausted.Exhausted = true
	require.Equal(t, exhausted, stored, "reissue may only mark the pinned request exhausted")
}
