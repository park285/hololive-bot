package dispatchoutbox

import (
	"embed"
	jsonv2 "encoding/json/v2"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

// 이전 SQL과 같은 fixture를 각각 새 DB에서 실행해 분기 이관의 전체 호출 비용을 비교한다.
//
//go:embed testdata/failure_routing_sql_policy.sql
var failureRoutingSQLPolicyAssets embed.FS

var failureRoutingSQLPolicy = sqlassets.MustReader(failureRoutingSQLPolicyAssets, "testdata")

func BenchmarkFailureRoutingPolicy(b *testing.B) {
	for _, size := range []int{1, 64, 512} {
		for _, sqlPolicy := range []bool{true, false} {
			name := "go_policy"

			if sqlPolicy {
				name = "sql_case"
			}

			b.Run(fmt.Sprintf("rows_%d/%s", size, name), func(b *testing.B) {
				runFailureRoutingBenchmark(b, size, sqlPolicy)
			})
		}
	}
}

func runFailureRoutingBenchmark(b *testing.B, size int, sqlPolicy bool) {
	b.Helper()

	pool, repo, updates := prepareFailureRoutingBenchmark(b, size)

	var payload any = updates

	if !sqlPolicy {
		payload = prepareFailureUpdates(updates)
	}

	encoded, err := jsonv2.Marshal(payload)
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

		if sqlPolicy {
			applyFailureSQLBenchmark(b, pool, updates)
		} else if err := repo.RouteFailures(b.Context(), updates, "bench-worker"); err != nil {
			b.Fatal(err)
		}
	}

	b.ReportMetric(float64(len(encoded)), "payload-B/op")
}

func applyFailureSQLBenchmark(b *testing.B, pool *pgxpool.Pool, updates []FailureUpdate) {
	b.Helper()

	for i := range updates {
		if updates[i].TargetStatus != StatusRetry && updates[i].TargetStatus != StatusDLQ {
			b.Fatalf("unsupported target status %q", updates[i].TargetStatus)
		}
	}

	raw, err := jsonv2.Marshal(updates)
	if err != nil {
		b.Fatal(err)
	}

	rows, err := pool.Query(b.Context(), failureRoutingSQLPolicy("failure_routing_sql_policy.sql"), jsonbRecordsetParam(raw), "bench-worker")
	if err != nil {
		b.Fatal(err)
	}

	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		b.Fatal(err)
	}

	if len(ids) != len(updates) {
		b.Fatalf("applied %d, want %d", len(ids), len(updates))
	}
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

		updates[i] = FailureUpdate{ID: id, AttemptCount: 1, NextAttemptAt: now.Add(time.Minute), Error: "benchmark provider failure", ErrorCode: "PROVIDER_FAILED", TargetStatus: status}
	}

	return pool, repo, updates
}
