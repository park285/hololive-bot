package dbtest

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

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
		Status:    dispatchoutbox.StatusPending,
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
		Status:    dispatchoutbox.StatusPending,
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
		Status:    dispatchoutbox.StatusPending,
	})
	require.NoError(t, err)
	require.Equal(t, len(envelopes), result.InsertedDeliveries)

	claimed, err := repository.ClaimDue(t.Context(), "worker-budget", 3, time.Minute)

	require.NoError(t, err)
	require.Len(t, claimed, 3)
}

func TestAlarmDispatchClaimDrainsLegacyBeforeSendUnits(t *testing.T) {
	pool := NewPool(t)
	repository := dispatchoutbox.NewPgxRepositoryFromPool(pool, nil)
	_, err := repository.InsertBatch(t.Context(), dispatchoutbox.PublishBatchInput{
		Envelopes: []domain.AlarmQueueEnvelope{sendUnitTestEnvelope("room-unit", "stream-unit")},
		Status:    dispatchoutbox.StatusPending,
	})
	require.NoError(t, err)

	var eventID int64

	require.NoError(t, pool.QueryRow(t.Context(), `
		INSERT INTO alarm_dispatch_events (
			event_key, payload_hash, alarm_type, channel_id, stream_id, category, payload
		) VALUES ($1, repeat('a', 64), 'LIVE', 'legacy-channel', 'legacy-stream', 'legacy', '{}'::jsonb)
		RETURNING id`, "legacy-event-"+fmt.Sprint(time.Now().UnixNano())).Scan(&eventID))

	var legacyID int64

	require.NoError(t, pool.QueryRow(t.Context(), `
		INSERT INTO alarm_dispatch_deliveries (event_id, room_id, dedupe_key, status, next_attempt_at)
		VALUES ($1, 'room-legacy', $2, 'pending', NOW() - INTERVAL '1 minute')
		RETURNING id`, eventID, "legacy-dedupe-"+fmt.Sprint(time.Now().UnixNano())).Scan(&legacyID))

	claimed, err := repository.ClaimDue(t.Context(), "worker-legacy", 10, time.Minute)

	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, legacyID, claimed[0].ID)
	require.Zero(t, claimed[0].SendUnitID)
}

func TestAlarmDispatchShadowDoesNotAllocateSendUnit(t *testing.T) {
	pool := NewPool(t)
	repository := dispatchoutbox.NewPgxRepositoryFromPool(pool, nil)
	result, err := repository.InsertBatch(t.Context(), dispatchoutbox.PublishBatchInput{
		Envelopes: []domain.AlarmQueueEnvelope{sendUnitTestEnvelope("room-shadow", "stream-shadow")},
		Status:    dispatchoutbox.StatusShadowed,
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.InsertedDeliveries)

	var (
		unitCount            int
		groupKey, sendUnitID any
	)

	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM alarm_dispatch_send_units").Scan(&unitCount))
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT dispatch_group_key, send_unit_id FROM alarm_dispatch_deliveries").Scan(&groupKey, &sendUnitID))
	require.Zero(t, unitCount)
	require.Nil(t, groupKey)
	require.Nil(t, sendUnitID)
}

func TestAlarmDispatchShadowPromotesToPendingOnCutover(t *testing.T) {
	pool := NewPool(t)
	repository := dispatchoutbox.NewPgxRepositoryFromPool(pool, nil)
	envelope := sendUnitTestEnvelope("room-shadow-cutover", "stream-shadow-cutover")
	shadow, err := repository.InsertBatch(t.Context(), dispatchoutbox.PublishBatchInput{
		Envelopes: []domain.AlarmQueueEnvelope{envelope},
		Status:    dispatchoutbox.StatusShadowed,
	})
	require.NoError(t, err)
	require.Equal(t, 1, shadow.InsertedDeliveries)

	cutover, err := repository.InsertBatch(t.Context(), dispatchoutbox.PublishBatchInput{
		Envelopes: []domain.AlarmQueueEnvelope{envelope, envelope},
		Status:    dispatchoutbox.StatusPending,
	})
	require.NoError(t, err)
	require.Equal(t, 1, cutover.InsertedDeliveries)
	require.Equal(t, 1, cutover.DuplicateDeliveries)

	var (
		status          string
		sendUnitID      int64
		clientRequestID string
	)

	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT d.status, d.send_unit_id, u.client_request_id
		FROM alarm_dispatch_deliveries d
		JOIN alarm_dispatch_send_units u ON u.id = d.send_unit_id
	`).Scan(&status, &sendUnitID, &clientRequestID))
	require.Equal(t, string(dispatchoutbox.StatusPending), status)
	require.Positive(t, sendUnitID)
	require.NotEmpty(t, clientRequestID)

	claimed, err := repository.ClaimDue(t.Context(), "worker-shadow-cutover", 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, sendUnitID, claimed[0].SendUnitID)
	require.Equal(t, clientRequestID, claimed[0].ClientRequestID)
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
