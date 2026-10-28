package dispatchoutbox

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// 전역 등록 counter를 교체하거나 병렬 실행하지 않아 전체 suite에서도 delta를 격리합니다.
func TestConsumerMarkSendingRegisteredCounterWithPGFence(t *testing.T) {
	pool := dbtest.NewPool(t)
	repository := newTestRepository(t, pool)
	_, err := repository.InsertBatch(t.Context(), PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{receiptEnvelope("metric-room", "metric")}})
	require.NoError(t, err)

	consumer := mustNewConsumer(t, repository, nil, nil, WithWorkerID("metric-owner"))
	envelopes, err := consumer.DrainBatch(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, envelopes, 1)

	request, err := consumer.PinSendRequest(t.Context(), envelopes, SendRequest{Body: "counter regression", Route: SendRouteText})
	require.NoError(t, err)
	require.NotNil(t, request)

	counting := &consumerBeginSendingMetricRepository{Repository: repository}

	consumer.repository = counting

	before := consumerRegisteredMarkSendingFailures(t)
	require.NoError(t, consumer.MarkSending(t.Context(), nil))
	require.NoError(t, consumer.MarkSending(t.Context(), []domain.AlarmQueueEnvelope{}))
	require.Zero(t, counting.calls)
	require.InDelta(t, before, consumerRegisteredMarkSendingFailures(t), 0)

	consumer.workerID = "foreign-owner"
	err = consumer.MarkSending(t.Context(), envelopes)

	miss, ok := errors.AsType[*sendfence.MissError](err)
	require.True(t, ok, "foreign owner must surface a send fence miss, got %v", err)
	require.True(t, miss.Has(sendfence.ReasonOwnerChanged))
	require.Equal(t, 1, counting.calls, "one failed operation, no retry")
	require.InDelta(t, before+1, consumerRegisteredMarkSendingFailures(t), 0)

	var status string

	require.NoError(t, pool.QueryRow(t.Context(), "SELECT status FROM alarm_dispatch_deliveries WHERE id = $1", envelopes[0].DispatchOutboxID).Scan(&status))
	require.Equal(t, "leased", status)

	consumer.workerID = "metric-owner"
	require.NoError(t, consumer.MarkSending(t.Context(), envelopes))
	require.Equal(t, 2, counting.calls)
	require.InDelta(t, before+1, consumerRegisteredMarkSendingFailures(t), 0, "success must not increment failures")
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT status FROM alarm_dispatch_deliveries WHERE id = $1", envelopes[0].DispatchOutboxID).Scan(&status))
	require.Equal(t, "sending", status)
}

func TestConsumerMarkSendingRegisteredCounterPreservesErrorChains(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, &pgconn.PgError{Code: "08006", Message: "connection failure"}} {
		t.Run(cause.Error(), func(t *testing.T) {
			repository := &consumerBeginSendingMetricRepository{Repository: &consumerTestRepository{}, failure: fmt.Errorf("database operation: %w", cause)}
			consumer := mustNewConsumer(t, repository, nil, nil, WithWorkerID("metric-owner"))
			before := consumerRegisteredMarkSendingFailures(t)
			err := consumer.MarkSending(t.Context(), []domain.AlarmQueueEnvelope{{DispatchOutboxID: 1}, {DispatchOutboxID: 2}})
			require.ErrorIs(t, err, cause)

			if pgCause, ok := errors.AsType[*pgconn.PgError](cause); ok {
				pgErr, ok := errors.AsType[*pgconn.PgError](err)
				require.True(t, ok)
				require.Same(t, pgCause, pgErr)
			}

			require.Equal(t, 1, repository.calls)
			require.InDelta(t, before+1, consumerRegisteredMarkSendingFailures(t), 0, "failure is per operation, not per delivery")
		})
	}
}

func consumerRegisteredMarkSendingFailures(t *testing.T) float64 {
	t.Helper()

	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	for _, family := range families {
		if family.GetName() == "alarm_dispatch_pg_mark_sending_failed_total" {
			require.Len(t, family.Metric, 1)

			return family.Metric[0].GetCounter().GetValue()
		}
	}

	t.Fatal("registered alarm_dispatch_pg_mark_sending_failed_total not found")

	return 0
}

type consumerBeginSendingMetricRepository struct {
	Repository

	calls   int
	failure error
}

func (r *consumerBeginSendingMetricRepository) BeginSendRequest(ctx context.Context, unitID int64, claims []DeliveryClaim, owner, requestID string, lease time.Duration) error {
	r.calls++
	if r.failure != nil {
		return r.failure
	}

	if err := r.Repository.BeginSendRequest(ctx, unitID, claims, owner, requestID, lease); err != nil {
		return fmt.Errorf("begin send metric fixture: %w", err)
	}

	return nil
}
