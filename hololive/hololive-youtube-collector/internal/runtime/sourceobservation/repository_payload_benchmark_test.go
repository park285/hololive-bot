package sourceobservation

import (
	"fmt"
	"testing"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

// -benchtime=30x로 실행해 전환 전 PublishConsume과 비교한다. WAL·테이블 크기에는 사전 비용도 포함된다.
func BenchmarkPayloadDictionaryPublish(b *testing.B) {
	for _, scenario := range []struct {
		name  string
		reuse bool
	}{
		{name: "unique"}, {name: "repeated", reuse: true},
	} {
		b.Run(scenario.name, func(b *testing.B) {
			ctx := b.Context()
			pool := dbtest.NewPool(b)
			repo := NewRepository(pool)
			proof := seedPublishLease(ctx, b, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")

			var (
				firstBytes, lastBytes int64
				firstWAL, lastWAL     string
			)

			measure := func(bytes *int64, wal *string) {
				b.Helper()

				if err := pool.QueryRow(ctx, `
					SELECT pg_total_relation_size('source_observations') +
					       pg_total_relation_size('source_observation_payloads') +
					       pg_total_relation_size('source_observation_queue'),
					       pg_current_wal_lsn()::text
				`).Scan(bytes, wal); err != nil {
					b.Fatal(err)
				}
			}
			measure(&firstBytes, &firstWAL)
			b.ReportAllocs()
			b.ResetTimer()

			iteration := 0

			for b.Loop() {
				b.StopTimer()

				proof = advanceLease(ctx, b, pool, &proof)

				postID := "same-post"

				if !scenario.reuse {
					postID = fmt.Sprintf("post-%d", iteration)
				}

				input := publishInput(communityEnvelope(b, &proof, postID))
				b.StartTimer()

				if _, err := repo.PublishBatch(ctx, input); err != nil {
					b.Fatal(err)
				}

				iteration++
			}

			b.StopTimer()
			measure(&lastBytes, &lastWAL)

			if iteration > 0 {
				b.ReportMetric(float64(lastBytes-firstBytes)/float64(iteration), "relation-bytes/op")

				var walBytes float64

				if err := pool.QueryRow(ctx, `SELECT pg_wal_lsn_diff($1::pg_lsn, $2::pg_lsn)`, lastWAL, firstWAL).Scan(&walBytes); err != nil {
					b.Fatal(err)
				}

				b.ReportMetric(walBytes/float64(iteration), "wal-bytes/op")
			}
		})
	}
}
