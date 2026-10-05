package notificationdelivery

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

const testFirstPeriod = "first"

func TestFetchReadyAndLockPreservesRoomOrderAcrossWorkers(t *testing.T) {
	r := testRepository(t)
	ctx := t.Context()

	for _, item := range []delivery.OutboxItem{
		{Kind: domain.DeliveryKindMemberNewsWeekly, PeriodKey: testFirstPeriod, RoomID: testRoomA, Message: "a1"},
		{Kind: domain.DeliveryKindMemberNewsWeekly, PeriodKey: "second", RoomID: testRoomA, Message: "a2"},
		{Kind: domain.DeliveryKindMemberNewsWeekly, PeriodKey: testFirstPeriod, RoomID: "room-b", Message: "b1"},
		{Kind: domain.DeliveryKindMemberNewsWeekly, PeriodKey: testFirstPeriod, RoomID: "room-c", Message: "c1"},
	} {
		require.NoError(t, r.EnqueueBatch(ctx, []delivery.OutboxItem{item}))
	}

	first, err := r.fetchReadyAndLock(ctx, testWorkerA, 10, deliveryLease, []string{"room-c"}, nil)
	require.NoError(t, err)
	require.Len(t, first, 2)

	var a1 int64

	for _, item := range first {
		if item.RoomID == testRoomA {
			a1 = item.ID
			require.Equal(t, testFirstPeriod, item.PeriodKey)
		}
	}

	require.NotZero(t, a1)

	other, err := r.fetchReadyAndLock(ctx, testWorkerB, 10, deliveryLease, nil, nil)
	require.NoError(t, err)
	require.Len(t, other, 1)
	require.Equal(t, "room-c", other[0].RoomID)

	started, err := r.MarkSending(ctx, a1, testWorkerA, deliveryLease)
	require.NoError(t, err)
	require.True(t, started)

	_, err = r.pool.Exec(ctx, "UPDATE notification_delivery_outbox SET lock_expires_at=clock_timestamp()-INTERVAL '1 second' WHERE id=$1", a1)
	require.NoError(t, err)

	whileSending, err := r.fetchReadyAndLock(ctx, testWorkerB, 10, deliveryLease, nil, nil)
	require.NoError(t, err)
	require.Empty(t, whileSending)

	finished, err := r.MarkSent(ctx, a1, testWorkerA)
	require.NoError(t, err)
	require.True(t, finished)

	next, err := r.fetchReadyAndLock(ctx, testWorkerB, 10, deliveryLease, nil, nil)
	require.NoError(t, err)
	require.Len(t, next, 1)
	require.Equal(t, testRoomA, next[0].RoomID)
	require.Equal(t, "second", next[0].PeriodKey)
}

func TestFetchReadyAndLockDoesNotSkipLockedPredecessor(t *testing.T) {
	r := testRepository(t)
	ctx := t.Context()

	for _, period := range []string{testFirstPeriod, "second"} {
		require.NoError(t, r.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, period, testRoomA, "message"))
	}

	require.NoError(t, r.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, testFirstPeriod, "room-b", "message"))

	tx, err := r.pool.Begin(ctx)
	require.NoError(t, err)

	defer func() { require.NoError(t, tx.Rollback(ctx)) }()

	var locked int64

	require.NoError(t, tx.QueryRow(ctx, "SELECT id FROM notification_delivery_outbox WHERE room_id='room-a' ORDER BY next_attempt_at,created_at,id LIMIT 1 FOR UPDATE").Scan(&locked))

	claimed, err := r.fetchReadyAndLock(ctx, testWorkerB, 10, time.Minute, nil, nil)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, "room-b", claimed[0].RoomID)
}

// 짧은 backoff로 같은 poll 안에서 다시 due가 된 행은 이미 처리한 ID로 넘겨 재claim하지 않는다.
func TestFetchReadyAndLockSkipsAlreadyProcessedIDs(t *testing.T) {
	r := testRepository(t)
	ctx := t.Context()
	require.NoError(t, r.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, testFirstPeriod, testRoomA, "message"))

	first, err := r.fetchReadyAndLock(ctx, testWorkerA, 1, deliveryLease, nil, nil)
	require.NoError(t, err)
	require.Len(t, first, 1)

	ok, err := r.MarkFailed(ctx, first[0].ID, testWorkerA, first[0].AttemptCount, 3, time.Millisecond, "retry soon")
	require.NoError(t, err)
	require.True(t, ok)

	_, err = r.pool.Exec(ctx, "UPDATE notification_delivery_outbox SET next_attempt_at=clock_timestamp()-INTERVAL '1 second' WHERE id=$1", first[0].ID)
	require.NoError(t, err)

	skipped, err := r.fetchReadyAndLock(ctx, testWorkerA, 1, deliveryLease, nil, []int64{first[0].ID})
	require.NoError(t, err)
	require.Empty(t, skipped)

	again, err := r.fetchReadyAndLock(ctx, testWorkerA, 1, deliveryLease, nil, nil)
	require.NoError(t, err)
	require.Len(t, again, 1)
	require.Equal(t, first[0].ID, again[0].ID)
}
