package sourceobservation

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo"
)

func BenchmarkPublishConsumeCommunityObservation(b *testing.B) {
	ctx := context.Background()
	pool := dbtest.NewPool(b)
	repository := NewRepository(pool)
	consumer := NewConsumer(
		repository,
		NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)),
		nil,
	)
	proof := seedPublishLease(
		b,
		pool,
		contract.ProviderYouTubeJS,
		contract.KindCommunityPage,
		"UC_TEST",
		"community_collect",
	)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i > 0 {
			b.StopTimer()
			proof = advanceLease(b, pool, proof, time.Minute)
			b.StartTimer()
		}
		envelope := communityEnvelope(
			b,
			proof,
			1,
			contract.CompletenessComplete,
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
