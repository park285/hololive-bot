package collectorruntime

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/sourceobservation"
)

func BenchmarkObservePublished(b *testing.B) {
	for _, size := range []int{1024, 64 * 1024, 1024 * 1024} {
		b.Run(fmt.Sprintf("bytes=%d", size), func(b *testing.B) {
			output, err := collectutil.NewRunOutput([]contract.Envelope{{
				Provider: contract.ProviderYouTubeJS, ObservationKind: contract.KindVideoList,
				Completeness: contract.CompletenessComplete, Continuity: contract.ContinuityContiguous,
				Payload: bytes.Repeat([]byte("x"), size),
			}}, []sourceobservation.CheckpointEntry{{}}, time.Second)
			if err != nil {
				b.Fatal(err)
			}

			executor := &collectionExecutor{metrics: NewMetrics(prometheus.NewRegistry())}
			result := sourceobservation.PublishBatchResult{Results: []sourceobservation.PublishedObservation{
				sourceobservation.NewPublishedObservation(1, sourceobservation.PublishInserted, 0),
			}}
			executor.observePublished(output, result)
			b.ReportAllocs()

			for b.Loop() {
				executor.observePublished(output, result)
			}
		})
	}
}
