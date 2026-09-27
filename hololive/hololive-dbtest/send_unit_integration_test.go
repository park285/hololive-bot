package dbtest

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
)

func TestAlarmDispatchSendUnitFirstInsertIsAtomic(t *testing.T) {
	pool := NewPool(t)
	repository := dispatchoutbox.NewPgxRepositoryFromPool(pool, nil)
	roomID := strings.Repeat("r", 100)
	envelope := sendUnitTestEnvelope(roomID, "stream-first")

	result, err := repository.InsertBatch(t.Context(), dispatchoutbox.PublishBatchInput{
		Envelopes: []domain.AlarmQueueEnvelope{envelope},
	})

	require.NoError(t, err)
	require.Equal(t, 1, result.InsertedDeliveries)

	var deliveryCount, unitCount int

	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM alarm_dispatch_deliveries").Scan(&deliveryCount))
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM alarm_dispatch_send_units").Scan(&unitCount))
	require.Equal(t, 1, deliveryCount)
	require.Equal(t, 1, unitCount)
}

func TestAlarmDispatchSendUnitConcurrentClaimKeepsGroupAtomic(t *testing.T) {
	pool := NewPool(t)
	repository := dispatchoutbox.NewPgxRepositoryFromPool(pool, nil)
	envelopes := []domain.AlarmQueueEnvelope{
		sendUnitTestEnvelope("room-group", "stream-a"),
		sendUnitTestEnvelope("room-group", "stream-b"),
		sendUnitTestEnvelope("room-group", "stream-c"),
	}
	result, err := repository.InsertBatch(t.Context(), dispatchoutbox.PublishBatchInput{
		Envelopes: envelopes,
	})
	require.NoError(t, err)
	require.Equal(t, len(envelopes), result.InsertedDeliveries)

	workers := []string{"worker-a", "worker-b"}
	claimed := make([][]*dispatchoutbox.Record, len(workers))
	errs := make([]error, len(workers))
	start := make(chan struct{})

	var ready, done sync.WaitGroup

	ready.Add(len(workers))
	done.Add(len(workers))

	for i := range workers {
		go func(index int) {
			defer done.Done()

			ready.Done()
			<-start

			claimed[index], errs[index] = repository.ClaimDue(t.Context(), workers[index], 10, time.Minute)
		}(i)
	}

	ready.Wait()
	close(start)
	done.Wait()

	claimingWorkers := 0

	for i := range workers {
		require.NoError(t, errs[i])

		if len(claimed[i]) == 0 {
			continue
		}

		claimingWorkers++

		require.Len(t, claimed[i], len(envelopes))

		unitID := claimed[i][0].SendUnitID
		clientRequestID := claimed[i][0].ClientRequestID

		require.Positive(t, unitID)
		require.NotEmpty(t, clientRequestID)

		for _, record := range claimed[i] {
			require.Equal(t, unitID, record.SendUnitID)
			require.Equal(t, clientRequestID, record.ClientRequestID)
			require.Equal(t, workers[i], record.LockedBy)
		}
	}

	require.Equal(t, 1, claimingWorkers)
}

func TestAlarmDispatchClaimUsesDeliveryBudgetAcrossUnits(t *testing.T) {
	pool := NewPool(t)
	repository := dispatchoutbox.NewPgxRepositoryFromPool(pool, nil)
	envelopes := make([]domain.AlarmQueueEnvelope, 0, 7)

	for i := range 7 {
		envelopes = append(envelopes, sendUnitTestEnvelope(fmt.Sprintf("room-budget-%d", i), fmt.Sprintf("stream-budget-%d", i)))
	}

	result, err := repository.InsertBatch(t.Context(), dispatchoutbox.PublishBatchInput{
		Envelopes: envelopes,
	})
	require.NoError(t, err)
	require.Equal(t, len(envelopes), result.InsertedDeliveries)

	claimed, err := repository.ClaimDue(t.Context(), "worker-budget", 3, time.Minute)

	require.NoError(t, err)
	require.Len(t, claimed, 3)
}

// 저장된 send unit 없는 migration 141 이전 delivery를 먼저 claim하던 legacy_head는 지웠다(stack-audit 2026-09-26 T17, T18에서
// 활성 NULL 행 0건 확인). 이제 claim이 읽지 않는 활성 NULL 행이 조용히 멈추지 않도록 migration 224의 CHECK가 쓰기 시점에 거절한다.
// 종단 NULL 행(T18 기준 sent·취소 상태 437건)은 retention 소거까지 유효하고, 수동 requeue로 활성 상태로 되돌리는 것은 거절된다.
func TestAlarmDispatchDeliveriesRequirePersistedSendUnitWhileActive(t *testing.T) {
	pool := NewPool(t)

	var eventID int64

	require.NoError(t, pool.QueryRow(t.Context(), `
		INSERT INTO alarm_dispatch_events (
			event_key, payload_hash, alarm_type, channel_id, stream_id, category, payload
		) VALUES ($1, repeat('a', 64), 'LIVE', 'legacy-channel', 'legacy-stream', 'legacy', '{}'::jsonb)
		RETURNING id`, "legacy-event-"+fmt.Sprint(time.Now().UnixNano())).Scan(&eventID))

	for _, status := range []string{"pending", "retry"} {
		_, err := pool.Exec(t.Context(), `
			INSERT INTO alarm_dispatch_deliveries (event_id, room_id, dedupe_key, status, next_attempt_at)
			VALUES ($1, 'room-legacy', $2, $3, NOW() - INTERVAL '1 minute')`,
			eventID, "legacy-active-"+status, status)
		requireActiveSendUnitViolation(t, err)
	}

	var terminalID int64

	require.NoError(t, pool.QueryRow(t.Context(), `
		INSERT INTO alarm_dispatch_deliveries (event_id, room_id, dedupe_key, status, dlq_at)
		VALUES ($1, 'room-legacy', 'legacy-terminal-dlq', 'dlq', NOW())
		RETURNING id`, eventID).Scan(&terminalID))

	_, err := pool.Exec(t.Context(), `
		UPDATE alarm_dispatch_deliveries
		SET status = 'retry', next_attempt_at = NOW()
		WHERE id = $1`, terminalID)
	requireActiveSendUnitViolation(t, err)
}

func requireActiveSendUnitViolation(t *testing.T, err error) {
	t.Helper()

	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	require.True(t, ok, "want active send unit check violation, got %v", err)
	require.Equal(t, "23514", pgErr.Code)
	require.Equal(t, "alarm_dispatch_deliveries_active_send_unit_check", pgErr.ConstraintName)
}

// migration 226이 v3 handoff의 비교 전용 shadowed 상태를 CHECK에서 뺐다(DEC-20260926-hololive-outbox-v3-convergence).
// 쓰기 경로가 다시 shadowed를 기록하면 DB가 거절해야 한다.
func TestAlarmDispatchDeliveryStatusRejectsShadowed(t *testing.T) {
	pool := NewPool(t)
	repository := dispatchoutbox.NewPgxRepositoryFromPool(pool, nil)
	result, err := repository.InsertBatch(t.Context(), dispatchoutbox.PublishBatchInput{
		Envelopes: []domain.AlarmQueueEnvelope{sendUnitTestEnvelope("room-status", "stream-status")},
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.InsertedDeliveries)

	_, err = pool.Exec(t.Context(), `UPDATE alarm_dispatch_deliveries SET status = 'shadowed'`)

	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	require.True(t, ok, "want status check violation, got %v", err)
	require.Equal(t, "23514", pgErr.Code)
	require.Equal(t, "alarm_dispatch_deliveries_status_check", pgErr.ConstraintName)
}

func sendUnitTestEnvelope(roomID, streamID string) domain.AlarmQueueEnvelope {
	start := time.Date(2026, time.August, 10, 12, 30, 0, 0, time.UTC)

	return domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType:    domain.AlarmTypeLive,
			RoomID:       roomID,
			Channel:      &domain.Channel{ID: "channel-send-unit"},
			Stream:       &domain.Stream{ID: streamID, ChannelID: "channel-send-unit", StartScheduled: &start},
			MinutesUntil: 10,
		},
		Version: 1,
	}
}
