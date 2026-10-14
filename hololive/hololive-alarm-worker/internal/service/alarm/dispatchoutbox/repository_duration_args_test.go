package dispatchoutbox

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 1초 미만 lease·quarantine 기준은 기본값으로 바꾸지 않고 DB 전이 전에 오류로 드러나야 한다.
// 저장소에 pool을 주지 않아 오류가 DB 접근보다 먼저 나는지도 함께 확인한다.
func TestPgxRepositoryRejectsSubSecondDurationsBeforeDatabase(t *testing.T) {
	t.Parallel()

	repository := NewPgxRepositoryFromPool(nil, nil)
	ids := []int64{1}
	calls := map[string]func(context.Context, time.Duration) error{
		"claim due": func(ctx context.Context, d time.Duration) error {
			_, err := repository.ClaimDue(ctx, "worker-1", 10, d)
			return err
		},
		"mark sending": func(ctx context.Context, d time.Duration) error {
			return repository.MarkSending(ctx, ids, "worker-1", d)
		},
		"begin send request": func(ctx context.Context, d time.Duration) error {
			return repository.BeginSendRequest(ctx, 1, ids, "worker-1", "request-1", d)
		},
		"quarantine stale sending": func(ctx context.Context, d time.Duration) error {
			_, err := repository.QuarantineStaleSending(ctx, d, 10)
			return err
		},
	}

	for name, call := range calls {
		for _, d := range []time.Duration{-time.Second, 0, 999 * time.Millisecond} {
			err := call(t.Context(), d)
			require.ErrorContains(t, err, "duration must be at least 1s", "%s(%s)", name, d)
		}
	}
}

func TestNewConsumerRejectsSubSecondLease(t *testing.T) {
	t.Parallel()

	_, err := NewConsumer(&consumerTestRepository{}, nil, nil, WithWorkerID("worker-1"), WithLease(500*time.Millisecond))
	require.ErrorContains(t, err, "lease must be at least 1s")

	consumer, err := NewConsumer(&consumerTestRepository{}, nil, nil, WithWorkerID("worker-1"), WithLease(time.Second))
	require.NoError(t, err)
	require.Equal(t, time.Second, consumer.lease)
}
