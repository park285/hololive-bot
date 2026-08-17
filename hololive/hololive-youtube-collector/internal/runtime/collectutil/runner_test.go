package collectutil

import (
	"context"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
)

func TestOutputAcceptsHololiveSizedBatch(t *testing.T) {
	t.Parallel()
	const hololiveSized = 90*4 + 1
	if hololiveSized > sourceobservation.MaxPublishBatchSize {
		t.Fatalf("publish limit %d cannot hold a Hololive-sized Holodex batch %d", sourceobservation.MaxPublishBatchSize, hololiveSized)
	}
	envelopes := make([]contract.Envelope, hololiveSized)
	output, err := OutputFromEnvelopes(envelopes, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(output.Observations()) != hololiveSized || len(output.Checkpoints()) != hololiveSized {
		t.Fatalf("output sizes = %d/%d", len(output.Observations()), len(output.Checkpoints()))
	}
}

func TestPaginationOfRejectsImpossibleTupleAsProtocolFault(t *testing.T) {
	t.Parallel()
	_, _, err := PaginationOf(&youtubejs.Pagination{
		PageCount:         1,
		Exhausted:         true,
		Continuity:        "",
		TerminationReason: youtubejs.TerminationExhausted,
	})
	if err == nil {
		t.Fatal("empty continuity must fail closed")
	}
	if collecterr.CodeOf(err) != collecterr.HelperProtocolMismatch {
		t.Fatalf("error code = %q, want helper protocol mismatch", collecterr.CodeOf(err))
	}
}

func TestOutputRejectsBatchAbovePublishLimit(t *testing.T) {
	t.Parallel()
	envelopes := make([]contract.Envelope, sourceobservation.MaxPublishBatchSize+1)
	if _, err := OutputFromEnvelopes(envelopes, time.Now()); err == nil {
		t.Fatal("expected publish limit rejection")
	}
}

func TestRunOutputDefensivelyClonesPayloadAndCursor(t *testing.T) {
	t.Parallel()
	envelopes := []contract.Envelope{{Payload: []byte(`{"value":1}`)}}
	checkpoints := []sourceobservation.CheckpointEntry{{Cursor: []byte(`{"next":"x"}`)}}
	output, err := NewRunOutput(envelopes, checkpoints, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	envelopes[0].Payload[0] = 'x'
	checkpoints[0].Cursor[0] = 'x'
	gotEnvelopes := output.Observations()
	gotCheckpoints := output.Checkpoints()
	gotEnvelopes[0].Payload[0] = 'y'
	gotCheckpoints[0].Cursor[0] = 'y'
	if string(output.Observations()[0].Payload) != `{"value":1}` ||
		string(output.Checkpoints()[0].Cursor) != `{"next":"x"}` {
		t.Fatal("RunOutput mutable bytes escaped")
	}
}

func TestRunOutputEmptyClonesAreNonNil(t *testing.T) {
	t.Parallel()
	output, err := NewRunOutput(nil, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if output.Observations() == nil {
		t.Fatal("empty observations clone is nil")
	}
	if output.Checkpoints() == nil {
		t.Fatal("empty checkpoints clone is nil")
	}
}

func TestPartialResultRejectsFatalOnlyClasses(t *testing.T) {
	t.Parallel()
	output, err := NewRunOutput(
		[]contract.Envelope{{ObservationKind: contract.KindVideoList}},
		[]sourceobservation.CheckpointEntry{{}},
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, cause := range []error{
		context.Canceled,
		collecterr.New(collecterr.HelperProtocolMismatch, collecterr.ClassProtocol, "protocol"),
		collecterr.New(collecterr.Internal, collecterr.ClassInternal, "internal"),
	} {
		if result, resultErr := NewPartialResult(output, cause, contract.KindShortsList); resultErr == nil || !result.IsZero() {
			t.Fatalf("fatal-only cause accepted as PARTIAL: %v", cause)
		}
	}
}
