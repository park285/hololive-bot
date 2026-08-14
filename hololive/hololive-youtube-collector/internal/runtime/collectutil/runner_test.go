package collectutil

import (
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
)

func TestOutputAcceptsHololiveSizedBatch(t *testing.T) {
	t.Parallel()
	const hololiveSized = 90*4 + 1
	if hololiveSized > sourceobservation.MaxPublishBatchSize {
		t.Fatalf("publish limit %d cannot hold a Hololive-sized Holodex batch %d", sourceobservation.MaxPublishBatchSize, hololiveSized)
	}
	envelopes := make([]contract.Envelope, hololiveSized)
	output, err := Output(envelopes, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(output.Observations) != hololiveSized || len(output.Checkpoints) != hololiveSized {
		t.Fatalf("output sizes = %d/%d", len(output.Observations), len(output.Checkpoints))
	}
}

func TestOutputRejectsBatchAbovePublishLimit(t *testing.T) {
	t.Parallel()
	envelopes := make([]contract.Envelope, sourceobservation.MaxPublishBatchSize+1)
	if _, err := Output(envelopes, time.Now()); err == nil {
		t.Fatal("expected publish limit rejection")
	}
}
