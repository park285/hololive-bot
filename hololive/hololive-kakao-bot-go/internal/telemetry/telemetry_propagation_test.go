package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func TestMapCarrier_BasicOperations(t *testing.T) {
	t.Parallel()

	carrier := MapCarrier{}
	carrier.Set("k1", "v1")
	carrier.Set("k2", "v2")

	assert.Equal(t, "v1", carrier.Get("k1"))
	assert.Equal(t, "", carrier.Get("missing"))

	keys := carrier.Keys()
	assert.Len(t, keys, 2)
	assert.ElementsMatch(t, []string{"k1", "k2"}, keys)
}

func TestInjectExtractContext_RoundTrip(t *testing.T) {
	prevPropagator := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTextMapPropagator(prevPropagator)
	})

	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:     trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8},
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), spanCtx)

	carrier := MapCarrier{}
	InjectContext(ctx, carrier)
	require.NotEmpty(t, carrier.Get("traceparent"))

	extractedCtx := ExtractContext(context.Background(), carrier)
	extractedSpan := trace.SpanContextFromContext(extractedCtx)
	require.True(t, extractedSpan.IsValid())
	assert.Equal(t, spanCtx.TraceID(), extractedSpan.TraceID())
	assert.Equal(t, spanCtx.SpanID(), extractedSpan.SpanID())
}

func TestProviderIsEnabled_WithTracerProvider(t *testing.T) {
	t.Parallel()

	tp := sdktrace.NewTracerProvider()
	provider := &Provider{tracerProvider: tp}

	assert.True(t, provider.IsEnabled())
	require.NoError(t, provider.Shutdown(context.Background()))
}

func TestExtractContext_EmptyCarrier(t *testing.T) {
	t.Parallel()

	ctx := ExtractContext(context.Background(), MapCarrier{})
	spanCtx := trace.SpanContextFromContext(ctx)
	assert.False(t, spanCtx.IsValid())
}
