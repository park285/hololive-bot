package collectutil

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/sourceobservation"
)

func BenchmarkCollectResult(b *testing.B) {
	for _, size := range []int{1024, 64 * 1024, 1024 * 1024} {
		for _, partial := range []bool{false, true} {
			b.Run(fmt.Sprintf("bytes=%d/partial=%t", size, partial), func(b *testing.B) {
				observations := []contract.Envelope{{
					ObservationKind: contract.KindVideoList,
					Payload:         bytes.Repeat([]byte("x"), size),
				}}
				checkpoints := []sourceobservation.CheckpointEntry{{Cursor: []byte(`{"next":"page"}`)}}
				cause := collecterr.New(collecterr.Failed, collecterr.ClassTransient, "shorts failed")

				b.ReportAllocs()

				for b.Loop() {
					output, err := NewRunOutput(observations, checkpoints, time.Second)
					if err != nil {
						b.Fatal(err)
					}

					var result CollectResult

					if partial {
						result, err = NewPartialResult(output, cause, contract.KindShortsList)
					} else {
						result, err = NewCompleteResult(output)
					}

					if err != nil {
						b.Fatal(err)
					}

					published := result.Output()
					if len(published.Observations()) != 1 || len(published.Checkpoints()) != 1 {
						b.Fatal("publish input was lost")
					}
				}
			})
		}
	}
}
