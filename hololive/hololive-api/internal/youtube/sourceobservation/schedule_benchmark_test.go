package sourceobservation

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/youtube/reconcile/schedule"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

func BenchmarkScheduleReconcile(b *testing.B) {
	benchmarkScheduleReconcile(b, reconcileScheduleBenchmark)
}

type scheduleBenchmarkOperation func(context.Context, dbx.Tx, *Observation, *schedule.Evidence) error

func benchmarkScheduleReconcile(b *testing.B, operation scheduleBenchmarkOperation) {
	b.Helper()

	for _, delay := range []time.Duration{0, time.Millisecond} {
		for _, size := range []int{0, 1, 64, 129, 1000} {
			b.Run(fmt.Sprintf("write_delay=%s/items=%d", delay, size), func(b *testing.B) {
				pool, writes := scheduleBenchmarkPool(b, delay, 1)
				observation, decision := scheduleBatchFixture(size)
				evidence := schedule.Evidence{
					GroupKey: "schedule-batch", Provider: observation.Provider, Items: decision.Items,
					EffectiveAt: observation.EffectiveAt, ReceivedAt: observation.EffectiveAt.Add(time.Second),
				}
				run := func(tx dbx.Tx) error {
					return operation(b.Context(), tx, &observation, &evidence)
				}
				// 같은 연결의 문장 준비와 기존 행 갱신을 측정 전에 수행한다.
				for range 2 {
					advanceScheduleBenchmark(&observation, &evidence)
					require.NoError(b, dbx.InPgxTx(b.Context(), pool, run))
				}

				writes.Store(0)
				b.ReportAllocs()

				for b.Loop() {
					advanceScheduleBenchmark(&observation, &evidence)

					if err := dbx.InPgxTx(b.Context(), pool, run); err != nil {
						b.Fatal(err)
					}
				}

				b.ReportMetric(float64(writes.Load())/float64(b.N), "wire-writes/op")
			})
		}
	}
}

func reconcileScheduleBenchmark(ctx context.Context, tx dbx.Tx, observation *Observation, evidence *schedule.Evidence) error {
	if err := lockScheduleSubject(ctx, tx, evidence.GroupKey); err != nil {
		return err
	}

	state, err := loadScheduleState(ctx, tx, evidence.Items)
	if err != nil {
		return err
	}

	decision, err := schedule.Reduce(state, *evidence)
	if err != nil {
		return fmt.Errorf("reduce schedule benchmark: %w", err)
	}

	return persistScheduleDecision(ctx, tx, observation, &decision)
}

func advanceScheduleBenchmark(observation *Observation, evidence *schedule.Evidence) {
	observation.EffectiveAt = observation.EffectiveAt.Add(time.Second)
	evidence.EffectiveAt = observation.EffectiveAt
	evidence.ReceivedAt = evidence.ReceivedAt.Add(time.Second)
}

func scheduleBenchmarkPool(b *testing.B, delay time.Duration, connections int32) (*pgxpool.Pool, *atomic.Int64) {
	b.Helper()

	base := dbtest.NewPool(b)
	config := base.Config()

	config.MinConns, config.MaxConns = connections, connections

	dial := config.ConnConfig.DialFunc
	writes := &atomic.Int64{}

	config.ConnConfig.DialFunc = func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := dial(ctx, network, address)
		if err != nil {
			return nil, err
		}

		return &scheduleBenchmarkConn{Conn: conn, delay: delay, writes: writes}, nil
	}

	pool, err := pgxpool.NewWithConfig(b.Context(), config)
	require.NoError(b, err)
	b.Cleanup(pool.Close)

	return pool, writes
}

type scheduleBenchmarkConn struct {
	net.Conn

	delay  time.Duration
	writes *atomic.Int64
}

// 로컬 연결의 전송마다 고정 대기를 추가하는 민감도 시험이며 운영 RTT 측정은 아니다.
func (conn *scheduleBenchmarkConn) Write(data []byte) (int, error) {
	conn.writes.Add(1)

	if conn.delay > 0 {
		time.Sleep(conn.delay)
	}

	n, err := conn.Conn.Write(data)
	if err != nil {
		return n, fmt.Errorf("write schedule benchmark connection: %w", err)
	}

	return n, nil
}
