package alarmdispatch

import (
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestDefaultRecoveryPreservesImmutableSendUnitMembership(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	repo := dispatchoutbox.NewPgxRepositoryFromPool(pool, nil)
	seedExpiredPinnedRecoveryUnits(t, pool, repo)

	consumer, err := dispatchoutbox.NewConsumer(repo, requestClaimReleaser{}, nil, dispatchoutbox.WithWorkerID("new-owner"))
	require.NoError(t, err)

	sender := &immutableRequestSender{route: dispatchoutbox.SendRouteText}
	runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: sender, maxBatch: 50}

	for range 2 {
		_, err = runner.runOnce(ctx)
		require.NoError(t, err)
	}

	var q, sent, started int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE status='quarantined'),count(*) FILTER(WHERE status='sent'),count(*) FILTER(WHERE sending_started_at IS NOT NULL) FROM alarm_dispatch_deliveries WHERE room_id='review-pair'`).Scan(&q, &sent, &started))
	require.Zero(t, q)
	require.Zero(t, sent)

	recovered, err := repo.RecoverExpiredLeased(ctx, 100)
	require.NoError(t, err)
	require.Equal(t, 2, recovered, "limit까지 남은 한 자리 때문에 같은 발송 단위를 분할하면 안 된다")

	_, err = runner.runOnce(ctx)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE status='quarantined'),count(*) FILTER(WHERE status='sent'),count(*) FILTER(WHERE sending_started_at IS NOT NULL) FROM alarm_dispatch_deliveries WHERE room_id='review-pair'`).Scan(&q, &sent, &started))
	require.Zero(t, q)
	require.Equal(t, 2, sent)
	require.Equal(t, 2, started)
	require.Len(t, sender.messages, 100)
}

func seedExpiredPinnedRecoveryUnits(t *testing.T, pool *pgxpool.Pool, repo *dispatchoutbox.PgxRepository) {
	t.Helper()

	ctx := t.Context()
	singles := make([]domain.AlarmQueueEnvelope, 99)

	for i := range singles {
		singles[i] = alarmDispatchRunnerTestEnvelope(fmt.Sprintf("review-room-%d", i), nil)
		singles[i].Notification.Stream.ID = fmt.Sprintf("review-stream-%d", i)
		singles[i].Notification.Channel.ID = "review-channel"
	}

	_, err := repo.InsertBatch(ctx, dispatchoutbox.PublishBatchInput{Envelopes: singles})
	require.NoError(t, err)

	pair := []domain.AlarmQueueEnvelope{alarmDispatchRunnerTestEnvelope("review-pair", nil), alarmDispatchRunnerTestEnvelope("review-pair", nil)}
	for i := range pair {
		pair[i].Notification.Stream.ID = fmt.Sprintf("review-pair-stream-%d", i)
		pair[i].Notification.Channel.ID = "review-channel"
	}

	_, err = repo.InsertBatch(ctx, dispatchoutbox.PublishBatchInput{Envelopes: pair})
	require.NoError(t, err)

	rows, err := repo.ClaimDue(ctx, "old-owner", 200, time.Minute)
	require.NoError(t, err)
	require.Len(t, rows, 101)

	units := map[int64][]int64{}

	for _, row := range rows {
		units[row.SendUnitID] = append(units[row.SendUnitID], row.ID)
	}

	require.Len(t, units, 100)

	for unit, ids := range units {
		_, err = repo.PinSendRequest(ctx, unit, ids, "old-owner", dispatchoutbox.SendRequest{Body: "never sent", Route: dispatchoutbox.SendRouteText})
		require.NoError(t, err)
	}

	_, err = pool.Exec(ctx, `UPDATE alarm_dispatch_deliveries SET locked_at=NOW()-INTERVAL '2 minutes',lock_expires_at=NOW()-INTERVAL '1 minute'`)
	require.NoError(t, err)
}
