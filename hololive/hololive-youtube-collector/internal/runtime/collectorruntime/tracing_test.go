package collectorruntime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
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
		{"fence_lost", collection.ErrFenceLost, "outcome_unknown", codes.Error},
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

func newCollectionTraceRecorder(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()

	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		require.NoError(t, provider.Shutdown(context.WithoutCancel(t.Context())))
	})

	return recorder
}

func assertPartialCollectionSpans(t *testing.T, spans []sdktrace.ReadOnlySpan) {
	t.Helper()

	wanted := map[string]string{"youtube.collection.attempt": "partial", "youtube.collection.fetch": "partial", "youtube.collection.publish": "success"}

	for _, span := range spans {
		want, ok := wanted[span.Name()]
		if !ok {
			continue
		}

		attrs := make(map[string]string)

		for _, attr := range span.Attributes() {
			attrs[string(attr.Key)] = attr.Value.AsString()
		}

		require.Equal(t, want, attrs["collection.outcome"], span.Name())

		if want == "partial" {
			require.Equal(t, codes.Error, span.Status().Code, span.Name())
			require.Equal(t, string(collecterr.ClassTimeout), attrs["error.type"], span.Name())
		} else {
			require.Equal(t, codes.Ok, span.Status().Code, span.Name())
		}

		delete(wanted, span.Name())
	}

	require.Empty(t, wanted, "required collection spans were not recorded")
}
