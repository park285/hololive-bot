package sourceobservation

import (
	"fmt"
	"testing"
	"time"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation/observationtest"
)

// Run with -benchtime=30x and compare both cases to the pre-cutover
// PublishConsume benchmark; WAL and relation bytes include dictionary overhead.
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
			proof := observationtest.SeedPublishLease(ctx, b, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")

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

				proof = observationtest.AdvanceLease(ctx, b, pool, &proof, time.Minute)

				postID := "same-post"

				if !scenario.reuse {
					postID = fmt.Sprintf("post-%d", iteration)
				}

				input := publishInput(observationtest.CommunityEnvelope(b, &proof, postID))
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
