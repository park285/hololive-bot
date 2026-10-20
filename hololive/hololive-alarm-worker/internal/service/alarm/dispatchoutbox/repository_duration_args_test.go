package dispatchoutbox

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// 1ms 미만 lease·quarantine 기준은 기본값으로 바꾸지 않고 DB 전이 전에 오류로 드러나야 한다.
// 저장소에 pool을 주지 않아 오류가 DB 접근보다 먼저 나는지도 함께 확인한다.
func TestPgxRepositoryRejectsSubMillisecondDurationsBeforeDatabase(t *testing.T) {
	t.Parallel()

	repository := NewPgxRepositoryFromPool(nil, nil)
	calls := map[string]func(context.Context, time.Duration) error{
		"claim due": func(ctx context.Context, d time.Duration) error {
			_, err := repository.ClaimDue(ctx, "worker-1", 10, d)
			return err
		},
		"begin send request": func(ctx context.Context, d time.Duration) error {
			return repository.BeginSendRequest(ctx, 1, attemptClaims(0, 1), "worker-1", "request-1", d)
		},
		"quarantine stale sending": func(ctx context.Context, d time.Duration) error {
			_, err := repository.QuarantineStaleSending(ctx, d, 10)
			return err
		},
	}

	for name, call := range calls {
		for _, d := range []time.Duration{-time.Millisecond, 0, 999 * time.Microsecond} {
			err := call(t.Context(), d)
			require.ErrorContains(t, err, "duration must be at least 1ms", "%s(%s)", name, d)
		}
	}
}

// 격리 기준은 유효하게 두어 lease 하한만으로 거부되는지 본다. 1ms 이상이면 초 단위가 아니어도 허용한다.
func TestNewConsumerRejectsSubMillisecondLease(t *testing.T) {
	t.Parallel()

	_, err := NewConsumer(&consumerTestRepository{}, nil, nil, WithWorkerID("worker-1"), WithLease(500*time.Microsecond), WithQuarantineThreshold(time.Minute))
	require.ErrorContains(t, err, "lease must be at least 1ms")

	_, err = NewConsumer(&consumerTestRepository{}, nil, nil, WithWorkerID("worker-1"), WithLease(1500*time.Millisecond), WithQuarantineThreshold(time.Minute))
	require.NoError(t, err)
}

// claim과 발송 진입 lease, stale 격리 기준은 초로 절사하지 않고 밀리초 그대로 DB 시각에 적용된다.
func TestPgxRepositoryKeepsMillisecondLeasesAndThresholds(t *testing.T) {
	pool := dbtest.NewPool(t)
	repository := NewPgxRepositoryFromPool(pool, nil)
	ctx := t.Context()
	_, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{receiptEnvelope("lease-ms-room", "lease-ms")}})
	require.NoError(t, err)

	records, err := repository.ClaimDue(ctx, "owner", 10, 1500*time.Millisecond)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, 1500*time.Millisecond, records[0].LockExpiresAt.Sub(*records[0].LockedAt))

	claims := recordClaims(records...)
	request, err := repository.PinSendRequest(ctx, records[0].SendUnitID, claims, "owner", records[0].ClientRequestID, SendRequest{Body: "lease ms", Route: SendRouteText})
	require.NoError(t, err)
	require.NoError(t, repository.BeginSendRequest(ctx, records[0].SendUnitID, claims, "owner", request.ClientRequestID, 2500*time.Millisecond))

	var sendingLease time.Duration

	require.NoError(t, pool.QueryRow(ctx, `SELECT lock_expires_at - sending_started_at FROM alarm_dispatch_deliveries WHERE id = $1`, records[0].ID).Scan(&sendingLease))
	require.Equal(t, 2500*time.Millisecond, sendingLease)

	_, err = pool.Exec(ctx, `UPDATE alarm_dispatch_deliveries SET sending_started_at = clock_timestamp() - interval '1 second' WHERE id = $1`, records[0].ID)
	require.NoError(t, err)

	quarantined, err := repository.QuarantineStaleSending(ctx, time.Minute, 10)
	require.NoError(t, err)
	require.Zero(t, quarantined)

	quarantined, err = repository.QuarantineStaleSending(ctx, 500*time.Millisecond, 10)
	require.NoError(t, err)
	require.Equal(t, 1, quarantined, "a 500ms threshold must not truncate to zero or round up to a full second")
}
