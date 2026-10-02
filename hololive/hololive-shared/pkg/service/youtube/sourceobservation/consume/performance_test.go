package consume

import (
	"strconv"
	"testing"
	"time"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation/observationtest"
)

func BenchmarkPublishConsumeCommunityObservation(b *testing.B) {
	ctx := b.Context()
	pool := dbtest.NewPool(b)
	repository := newTestRepository(pool)
	consumer := NewConsumer(
		repository,
		NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)),
		nil,
	)
	proof := observationtest.SeedPublishLease(
		b.Context(),
		b,
		pool,
		contract.ProviderYouTubeJS,
		contract.KindCommunityPage,
		testChannelID,
		"community_collect",
	)
	b.ReportAllocs()
	b.ResetTimer()

	i := 0

	for b.Loop() {
		if i > 0 {
			b.StopTimer()

			proof = observationtest.AdvanceLease(b.Context(), b, pool, &proof, time.Minute)
			b.StartTimer()
		}

		envelope := observationtest.CommunityEnvelope(
			b,
			&proof,
			"perf-post-"+strconv.Itoa(i),
		)
		if _, err := repository.PublishBatch(ctx, publishInput(envelope)); err != nil {
			b.Fatal(err)
		}

		if err := consumer.Consume(ctx, claimOptions()); err != nil {
			b.Fatal(err)
		}

		i++
	}
}
