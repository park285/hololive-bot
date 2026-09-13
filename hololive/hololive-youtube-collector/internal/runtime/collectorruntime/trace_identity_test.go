package collectorruntime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/park285/shared-go/v2/pkg/telemetry"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

func TestCollectorTraceIdentitySeparatesAPsAndPreservesHealthFilter(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	previous := otel.GetTracerProvider()

	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		require.NoError(t, provider.Shutdown(context.WithoutCancel(t.Context())))
	})

	for _, instance := range []string{"youtube-collector-a", "youtube-collector-b", "youtube-collector-c", "youtube-collector-d"} {
		t.Run(instance, func(t *testing.T) {
			exporter.Reset()

			router, err := sharedserver.NewHealthOnlyRuntimeRouter(t.Context(), testLogger(), "test-key",
				func(options *sharedserver.RuntimeRouterOptions) {
					options.PreRouteUse = append(options.PreRouteUse, collectorTraceIdentity(instance).handle)
				})
			require.NoError(t, err)

			handler := telemetry.NewPublicHTTPHandler(router, runtimeName+"-http",
				telemetry.HTTPHandlerOptions{Filter: sharedserver.LocalPlaneTraceFilter})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/__observability/trace-heartbeat", http.NoBody))
			require.Equal(t, http.StatusNotFound, response.Code)

			spans := exporter.GetSpans()
			require.Len(t, spans, 1)

			found := false

			for _, attr := range spans[0].Attributes {
				if attr.Key == "youtube.collector.instance_id" {
					require.Equal(t, instance, attr.Value.AsString())

					found = true
				}
			}

			require.True(t, found, "heartbeat span must identify its owning AP")

			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health", http.NoBody))
			require.Len(t, exporter.GetSpans(), 1, "health requests remain excluded")
		})
	}
}
