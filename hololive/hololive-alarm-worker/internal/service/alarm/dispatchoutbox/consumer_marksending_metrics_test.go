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

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// 전역 등록 counter를 교체하거나 병렬 실행하지 않아 전체 suite에서도 delta를 격리합니다.
func TestConsumerMarkSendingRegisteredCounterWithPGFence(t *testing.T) {
	for _, immutable := range []bool{true, false} {
		t.Run(fmt.Sprintf("immutable=%t", immutable), func(t *testing.T) {
			pool := dbtest.NewPool(t)
			repository := NewPgxRepositoryFromPool(pool, nil)
			_, err := repository.InsertBatch(t.Context(), PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{receiptEnvelope("metric-room", "metric")}})
			require.NoError(t, err)

			consumer := mustNewConsumer(t, repository, nil, nil, WithWorkerID("metric-owner"))
			envelopes, err := consumer.DrainBatch(t.Context(), 10)
			require.NoError(t, err)
			require.Len(t, envelopes, 1)

			request, err := consumer.PinSendRequest(t.Context(), envelopes, SendRequest{Body: "counter regression", Route: SendRouteText})
			require.NoError(t, err)
			require.NotNil(t, request)

			envelopes[0].ClientRequestID = request.ClientRequestID

			fallback := &consumerMarkSendingMetricRepository{Repository: repository}

			consumer.repository = fallback

			if immutable {
				consumer.repository = &consumerBeginSendingMetricRepository{consumerMarkSendingMetricRepository: fallback, begin: repository.BeginSendRequest}
			}

			before := consumerRegisteredMarkSendingFailures(t)
			require.NoError(t, consumer.MarkSending(t.Context(), nil))
			require.NoError(t, consumer.MarkSending(t.Context(), []domain.AlarmQueueEnvelope{}))
			require.Zero(t, fallback.calls)
			require.InDelta(t, before, consumerRegisteredMarkSendingFailures(t), 0)

			consumer.workerID = "foreign-owner"
			err = consumer.MarkSending(t.Context(), envelopes)

			if immutable {
				require.ErrorIs(t, err, ErrSendRequestFence)
			} else {
				require.Error(t, err)
			}

			require.Equal(t, 1, fallback.calls, "one failed operation, no retry")
			require.InDelta(t, before+1, consumerRegisteredMarkSendingFailures(t), 0)

			var status string

			require.NoError(t, pool.QueryRow(t.Context(), "SELECT status FROM alarm_dispatch_deliveries WHERE id = $1", envelopes[0].DispatchOutboxID).Scan(&status))
			require.Equal(t, "leased", status)

			consumer.workerID = "metric-owner"
			require.NoError(t, consumer.MarkSending(t.Context(), envelopes))
			require.Equal(t, 2, fallback.calls)
			require.InDelta(t, before+1, consumerRegisteredMarkSendingFailures(t), 0, "success must not increment failures")
			require.NoError(t, pool.QueryRow(t.Context(), "SELECT status FROM alarm_dispatch_deliveries WHERE id = $1", envelopes[0].DispatchOutboxID).Scan(&status))
			require.Equal(t, "sending", status)
		})
	}
}

func TestConsumerMarkSendingRegisteredCounterPreservesErrorChains(t *testing.T) {
	for _, immutable := range []bool{true, false} {
		for _, cause := range []error{context.Canceled, context.DeadlineExceeded, &pgconn.PgError{Code: "08006", Message: "connection failure"}} {
			t.Run(fmt.Sprintf("immutable=%t/%s", immutable, cause), func(t *testing.T) {
				fallback := &consumerMarkSendingMetricRepository{Repository: &consumerTestRepository{}, failure: fmt.Errorf("database operation: %w", cause)}

				var repository Repository = fallback

				if immutable {
					repository = &consumerBeginSendingMetricRepository{consumerMarkSendingMetricRepository: fallback}
				}

				consumer := mustNewConsumer(t, repository, nil, nil, WithWorkerID("metric-owner"))
				before := consumerRegisteredMarkSendingFailures(t)
				err := consumer.MarkSending(t.Context(), []domain.AlarmQueueEnvelope{{DispatchOutboxID: 1}, {DispatchOutboxID: 2}})
				require.ErrorIs(t, err, cause)

				if pgCause, ok := errors.AsType[*pgconn.PgError](cause); ok {
					pgErr, ok := errors.AsType[*pgconn.PgError](err)
					require.True(t, ok)
					require.Same(t, pgCause, pgErr)
				}

				require.Equal(t, 1, fallback.calls)
				require.InDelta(t, before+1, consumerRegisteredMarkSendingFailures(t), 0, "failure is per operation, not per delivery")
			})
		}
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

type consumerMarkSendingMetricRepository struct {
	Repository

	calls   int
	failure error
}

func (r *consumerMarkSendingMetricRepository) MarkSending(ctx context.Context, ids []int64, owner string, lease time.Duration) error {
	r.calls++
	if r.failure != nil {
		return r.failure
	}

	if err := r.Repository.MarkSending(ctx, ids, owner, lease); err != nil {
		return fmt.Errorf("mark sending metric fixture: %w", err)
	}

	return nil
}

type consumerBeginSendingMetricRepository struct {
	*consumerMarkSendingMetricRepository

	begin func(context.Context, int64, []int64, string, string, time.Duration) error
}

func (r *consumerBeginSendingMetricRepository) BeginSendRequest(ctx context.Context, unitID int64, ids []int64, owner, requestID string, lease time.Duration) error {
	r.calls++
	if r.failure != nil {
		return r.failure
	}

	return r.begin(ctx, unitID, ids, owner, requestID, lease)
}
