package dispatchoutbox_test

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestAlarmDispatchSendUnitFirstInsertIsAtomic(t *testing.T) {
	pool := dbtest.NewPool(t)
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
	pool := dbtest.NewPool(t)
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

	for i := range workers {
		done.Go(func() {
			ready.Done()
			<-start

			claimed[i], errs[i] = repository.ClaimDue(t.Context(), workers[i], 10, time.Minute)
		})
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
	pool := dbtest.NewPool(t)
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

func TestAlarmDispatchDeliveryStatusRejectsShadowed(t *testing.T) {
	pool := dbtest.NewPool(t)
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
