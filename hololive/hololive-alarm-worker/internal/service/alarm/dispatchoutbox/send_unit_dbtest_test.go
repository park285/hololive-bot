package dispatchoutbox_test

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
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

// 같은 unit 멤버의 next_attempt_at이 1µs 갈리고 DB 시각이 그 사이에 있어도 claim은 unit 전체를 가져가고 발송
// fence를 통과해야 한다. 일부만 leased되면 fence가 unit을 격리한다.
func TestAlarmDispatchClaimTakesWholeUnitAcrossMemberDueSkew(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	at := time.Now().UTC().Truncate(time.Microsecond)
	repository := dispatchoutbox.NewPgxRepositoryFromPool(fixedNowPool(t, pool, at), nil)
	result, err := repository.InsertBatch(ctx, dispatchoutbox.PublishBatchInput{
		Envelopes: []domain.AlarmQueueEnvelope{
			sendUnitTestEnvelope("room-skew", "stream-skew-a"),
			sendUnitTestEnvelope("room-skew", "stream-skew-b"),
		},
	})
	require.NoError(t, err)
	require.Equal(t, 2, result.InsertedDeliveries)

	_, err = pool.Exec(ctx, `UPDATE alarm_dispatch_deliveries SET next_attempt_at = $1::timestamptz + INTERVAL '1 microsecond'
		WHERE id = (SELECT max(id) FROM alarm_dispatch_deliveries)`, at)
	require.NoError(t, err)

	const worker = "worker-skew"

	claimed, err := repository.ClaimDue(ctx, worker, 10, time.Minute)
	require.NoError(t, err)
	require.NotEmpty(t, claimed)

	unitID := claimed[0].SendUnitID
	claims := make([]dispatchoutbox.DeliveryClaim, 0, len(claimed))

	for _, record := range claimed {
		require.Equal(t, unitID, record.SendUnitID)

		claims = append(claims, dispatchoutbox.DeliveryClaim{ID: record.ID, AttemptCount: record.AttemptCount})
	}

	require.Len(t, claims, 2)

	request, err := repository.PinSendRequest(ctx, unitID, claims, worker, claimed[0].ClientRequestID, dispatchoutbox.SendRequest{Body: "body", Route: dispatchoutbox.SendRouteText, RoomID: "room-skew"})
	require.NoError(t, err)
	require.NoError(t, repository.BeginSendRequest(ctx, unitID, claims, worker, request.ClientRequestID, time.Minute))

	var sending int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM alarm_dispatch_deliveries WHERE status = 'sending' AND locked_by = $1`, worker).Scan(&sending))
	require.Equal(t, 2, sending)
}

// ready 멤버가 끌어오는 대상은 대기 멤버뿐이다. 종단 멤버는 claim으로 되살아나지 않는다.
func TestAlarmDispatchClaimLeavesTerminalUnitMembers(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	repository := dispatchoutbox.NewPgxRepositoryFromPool(pool, nil)
	result, err := repository.InsertBatch(ctx, dispatchoutbox.PublishBatchInput{
		Envelopes: []domain.AlarmQueueEnvelope{
			sendUnitTestEnvelope("room-terminal", "stream-terminal-a"),
			sendUnitTestEnvelope("room-terminal", "stream-terminal-b"),
			sendUnitTestEnvelope("room-terminal", "stream-terminal-c"),
		},
	})
	require.NoError(t, err)
	require.Equal(t, 3, result.InsertedDeliveries)

	var dlqID, futureID int64

	require.NoError(t, pool.QueryRow(ctx, `
		WITH ordered AS (SELECT id, row_number() OVER (ORDER BY id) AS position FROM alarm_dispatch_deliveries),
		dlq AS (
			UPDATE alarm_dispatch_deliveries d SET status = 'dlq', dlq_at = NOW()
			FROM ordered WHERE d.id = ordered.id AND ordered.position = 2 RETURNING d.id
		), future AS (
			UPDATE alarm_dispatch_deliveries d SET status = 'retry', next_attempt_at = NOW() + INTERVAL '1 hour'
			FROM ordered WHERE d.id = ordered.id AND ordered.position = 3 RETURNING d.id
		)
		SELECT dlq.id, future.id FROM dlq CROSS JOIN future`).Scan(&dlqID, &futureID))

	claimed, err := repository.ClaimDue(ctx, "worker-terminal", 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 2)

	claimedIDs := make([]int64, 0, len(claimed))
	for _, record := range claimed {
		claimedIDs = append(claimedIDs, record.ID)
	}

	require.Contains(t, claimedIDs, futureID)
	require.NotContains(t, claimedIDs, dlqID)

	var status string

	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM alarm_dispatch_deliveries WHERE id = $1`, dlqID).Scan(&status))
	require.Equal(t, "dlq", status)
}

// fixedNowPool은 now()와 clock_timestamp()가 모두 at을 돌려주는 연결 pool을 만든다. 검색 경로에서 pg_catalog보다
// 앞선 schema의 함수가 내장 함수를 가리므로 claim의 ready 판정과 fence 시각을 µs 단위로 결정할 수 있다.
func fixedNowPool(t *testing.T, pool *pgxpool.Pool, at time.Time) *pgxpool.Pool {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
		CREATE SCHEMA fixed_clock;
		CREATE TABLE fixed_clock.instant (at TIMESTAMPTZ NOT NULL);
		CREATE FUNCTION fixed_clock.now() RETURNS TIMESTAMPTZ LANGUAGE sql STABLE AS 'SELECT at FROM fixed_clock.instant';
		CREATE FUNCTION fixed_clock.clock_timestamp() RETURNS TIMESTAMPTZ LANGUAGE sql VOLATILE AS 'SELECT at FROM fixed_clock.instant'`)
	require.NoError(t, err)

	_, err = pool.Exec(t.Context(), `INSERT INTO fixed_clock.instant (at) VALUES ($1)`, at)
	require.NoError(t, err)

	config := pool.Config()

	config.ConnConfig.RuntimeParams["search_path"] = "fixed_clock, public, pg_catalog"

	clocked, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(clocked.Close)

	return clocked
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
