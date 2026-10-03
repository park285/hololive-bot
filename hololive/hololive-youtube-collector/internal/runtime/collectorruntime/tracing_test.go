package collectorruntime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
)

func TestCollectionSpanPreservesOutcome(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		outcome string
		status  codes.Code
	}{
		{"success", nil, resultSuccess, codes.Ok},
		{"failure", errors.New("private upstream detail"), resultFailed, codes.Error},
		{"canceled", context.Canceled, "canceled", codes.Error},
		{"timeout", context.DeadlineExceeded, "timeout", codes.Error},
		{"fence_lost", joblease.ErrFenceLost, "outcome_unknown", codes.Error},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

			t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.WithoutCancel(t.Context()))) })

			_, span := provider.Tracer("test").Start(t.Context(), "collection")

			finishCollectionSpan(span, tc.err)

			ended := recorder.Ended()
			require.Len(t, ended, 1)
			require.Equal(t, tc.status, ended[0].Status().Code)

			attributes := make(map[string]string)

			for _, attr := range ended[0].Attributes() {
				attributes[string(attr.Key)] = attr.Value.AsString()
			}

			require.Equal(t, tc.outcome, attributes["collection.outcome"])
			require.NotContains(t, ended[0].Status().Description, "private upstream detail")
			require.Empty(t, ended[0].Events())
		})
	}
}
