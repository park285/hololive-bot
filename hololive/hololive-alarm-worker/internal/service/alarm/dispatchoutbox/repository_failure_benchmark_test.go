package dispatchoutbox

import (
	jsonv2 "encoding/json/v2"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// 같은 fixture를 새 DB에서 실행해 실패 정산 한 번의 전체 호출 비용(직렬화·전송·관측 수집)을 잰다.
func BenchmarkFailureRoutingPolicy(b *testing.B) {
	for _, size := range []int{1, 64, 512} {
		b.Run(fmt.Sprintf("rows_%d", size), func(b *testing.B) {
			runFailureRoutingBenchmark(b, size)
		})
	}
}

func runFailureRoutingBenchmark(b *testing.B, size int) {
	b.Helper()

	pool, repo, updates := prepareFailureRoutingBenchmark(b, size)

	encoded, err := jsonv2.Marshal(prepareFailureUpdates(updates))
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()

	for b.Loop() {
		b.StopTimer()

		if _, err := pool.Exec(b.Context(), `UPDATE alarm_dispatch_deliveries SET status='leased',attempt_count=0,locked_by='bench-worker',locked_at=clock_timestamp(),lock_expires_at=clock_timestamp()+interval '1 minute',dlq_at=NULL`); err != nil {
			b.Fatal(err)
		}

		b.StartTimer()

		if err := repo.RouteFailures(b.Context(), updates, "bench-worker"); err != nil {
			b.Fatal(err)
		}
	}

	b.ReportMetric(float64(len(encoded)), "payload-B/op")
}

func prepareFailureRoutingBenchmark(b *testing.B, size int) (*pgxpool.Pool, *PgxRepository, []FailureUpdate) {
	b.Helper()

	pool := dbtest.NewPool(b)
	repo := NewPgxRepositoryFromPool(pool, nil)
	now := time.Now().UTC().Truncate(time.Microsecond)
	envelopes := make([]domain.AlarmQueueEnvelope, size)

	for i := range envelopes {
		envelopes[i] = domain.AlarmQueueEnvelope{Notification: *candidateNotification(fmt.Sprintf("bench-room-%d", i), now.Add(time.Hour)), Version: 1}
	}

	if _, err := repo.InsertBatch(b.Context(), PublishBatchInput{Envelopes: envelopes}); err != nil {
		b.Fatal(err)
	}

	rows, err := pool.Query(b.Context(), "SELECT id FROM alarm_dispatch_deliveries ORDER BY id")
	if err != nil {
		b.Fatal(err)
	}

	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		b.Fatal(err)
	}

	if len(ids) != size {
		b.Fatalf("fixture %d, want %d", len(ids), size)
	}

	updates := make([]FailureUpdate, len(ids))
	for i, id := range ids {
		status := StatusRetry

		if i%2 != 0 {
			status = StatusDLQ
		}

		updates[i] = FailureUpdate{ID: id, AttemptCount: 1, RetryAfter: time.Minute, Error: "benchmark provider failure", ErrorCode: "PROVIDER_FAILED", TargetStatus: status}
	}

	return pool, repo, updates
}
