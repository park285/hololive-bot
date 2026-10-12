package collectorruntime

import (
	"errors"
	"testing"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/sourceobservation"
)

func TestWrapPublishFailurePreservesFailureSemantics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		source    error
		wantCode  contract.CollectionErrorCode
		wantClass contract.FailureClass
	}{
		{name: "invalid envelope", source: sourceobservation.ErrInvalidEnvelope, wantCode: contract.ErrorInternalInvariant, wantClass: contract.ClassInternal},
		{name: "stale contract", source: sourceobservation.ErrStaleContract, wantCode: contract.ErrorPublishRejected, wantClass: contract.ClassProtocol},
		{name: "database failure", source: errors.New("database unavailable"), wantCode: contract.ErrorPublishRejected, wantClass: contract.ClassTransient},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := wrapPublishFailure("publish", test.source)
			if !errors.Is(err, test.source) {
				t.Fatalf("wrapped error does not preserve source: %v", err)
			}

			if got := collecterr.CodeOf(err); got != test.wantCode {
				t.Fatalf("CodeOf() = %q, want %q", got, test.wantCode)
			}

			if got := collecterr.ClassOf(err); got != test.wantClass {
				t.Fatalf("ClassOf() = %q, want %q", got, test.wantClass)
			}
		})
	}
}
