package sourceobservation

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/youtube/reconcile/schedule"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

func BenchmarkScheduleReconcileContended(b *testing.B) {
	benchmarkScheduleContention(b, reconcileScheduleBenchmark)
}

func benchmarkScheduleContention(b *testing.B, operation scheduleBenchmarkOperation) {
	b.Helper()

	const workers = 4

	for _, size := range []int{1, 129} {
		b.Run(fmt.Sprintf("workers=4/items=%d", size), func(b *testing.B) {
			pool, writes := scheduleBenchmarkPool(b, workers)
			observation, decision := scheduleBatchFixture(size)
			evidence := schedule.Evidence{
				GroupKey: "schedule-batch", Provider: observation.Provider, Items: decision.Items,
				EffectiveAt: observation.EffectiveAt, ReceivedAt: observation.EffectiveAt.Add(time.Second),
			}
			jobs, results := make(chan struct{}, workers), make(chan error, workers)

			var group sync.WaitGroup

			for range workers {
				group.Go(func() {
					for range jobs {
						results <- dbx.InPgxTx(b.Context(), pool, func(tx dbx.Tx) error {
							return operation(b.Context(), tx, &observation, &evidence)
						})
					}
				})
			}

			defer func() {
				close(jobs)
				group.Wait()
			}()

			round := func() {
				for range workers {
					jobs <- struct{}{}
				}

				for range workers {
					require.NoError(b, <-results)
				}
			}

			for range 2 {
				round()
			}

			writes.Store(0)
			b.ReportAllocs()

			for b.Loop() {
				round()
			}

			b.ReportMetric(float64(writes.Load())/float64(workers*b.N), "wire-writes/observation")
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(workers*b.N), "ns/observation")
		})
	}
}
