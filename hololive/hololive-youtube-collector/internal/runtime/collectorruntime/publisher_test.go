package collectorruntime

import (
	"errors"
	"testing"

	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

func TestWrapPublishFailurePreservesFailureSemantics(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		source    error
		wantCode  collecterr.ErrorCode
		wantClass collecterr.FailureClass
	}{
		{name: "invalid envelope", source: sourceobservation.ErrInvalidEnvelope, wantCode: collecterr.Internal, wantClass: collecterr.ClassInternal},
		{name: "stale contract", source: sourceobservation.ErrStaleContract, wantCode: collecterr.PublishRejected, wantClass: collecterr.ClassProtocol},
		{name: "database failure", source: errors.New("database unavailable"), wantCode: collecterr.PublishRejected, wantClass: collecterr.ClassTransient},
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
