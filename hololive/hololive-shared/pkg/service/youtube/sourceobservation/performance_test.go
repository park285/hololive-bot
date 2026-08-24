package sourceobservation

import (
	"strconv"
	"testing"
	"time"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo"
)

func BenchmarkPublishConsumeCommunityObservation(b *testing.B) {
	ctx := b.Context()
	pool := dbtest.NewPool(b)
	repository := NewRepository(pool)
	consumer := NewConsumer(
		repository,
		NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)),
		nil,
	)
	proof := seedPublishLease(
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

	for i := range b.N {
		if i > 0 {
			b.StopTimer()

			proof = advanceLease(b.Context(), b, pool, &proof, time.Minute)
			b.StartTimer()
		}

		envelope := communityEnvelope(
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
	}
}
