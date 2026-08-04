package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestOtelHandlerDoesNotRecordRawRequestTarget(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(recorder),
	)
	installRuntimeOTelGlobals(t, provider)

	const (
		operation     = "hololive.http.server"
		requestTarget = "/api/private/private-user-123?token=private-token-456"
	)

	var (
		gotURL        *url.URL
		gotRequestURI string
	)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/private/{userID}", func(w http.ResponseWriter, r *http.Request) {
		copiedURL := *r.URL
		gotURL = &copiedURL
		gotRequestURI = r.RequestURI
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, requestTarget, http.NoBody)
	req.URL.RawPath = "/api/private/private-user-%31%32%33"
	req.URL.ForceQuery = true
	req.URL.Fragment = "private-fragment-789"
	req.URL.RawFragment = "private-fragment-%37%38%39"
	req.URL.Opaque = "private-opaque-abc"
	wantURL := *req.URL

	newOtelHandler(mux, operation).ServeHTTP(httptest.NewRecorder(), req)

	if gotURL == nil {
		t.Fatal("handler was not called")
	}
	if *gotURL != wantURL {
		t.Fatalf("handler URL = %#v, want %#v", *gotURL, wantURL)
	}
	if gotRequestURI != requestTarget {
		t.Fatalf("handler RequestURI = %q, want %q", gotRequestURI, requestTarget)
	}

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	if got := spans[0].Name(); got != operation {
		t.Fatalf("span name = %q, want %q", got, operation)
	}

	sentinels := []string{
		"private-user-123",
		"private-user-%31%32%33",
		"private-token-456",
		"private-fragment-789",
		"private-fragment-%37%38%39",
		"private-opaque-abc",
	}
	for _, attr := range spans[0].Attributes() {
		value := attr.Value.String()
		for _, sentinel := range sentinels {
			if strings.Contains(value, sentinel) {
				t.Fatalf("request identifier %q recorded in span attribute %q", sentinel, attr.Key)
			}
		}
	}
}

func TestOtelHandlerRemoteSampledParentCannotOverrideLocalSampler(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(0.1))),
		sdktrace.WithSpanProcessor(recorder),
		sdktrace.WithIDGenerator(runtimeUnsampledIDGenerator{}),
	)
	installRuntimeOTelGlobals(t, provider)

	var (
		called  bool
		sampled bool
	)
	handler := newOtelHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		sampled = trace.SpanFromContext(r.Context()).SpanContext().IsSampled()
		w.WriteHeader(http.StatusNoContent)
	}), "hololive.http.server")
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/private", http.NoBody)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	if !called {
		t.Fatal("handler was not called")
	}
	if sampled {
		t.Fatal("remote sampled traceparent overrode the local sampler")
	}
	if got := len(recorder.Ended()); got != 0 {
		t.Fatalf("ended spans = %d, want 0", got)
	}
}

func TestOtelHandlerDoesNotExtractRemoteBaggage(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(recorder),
	)
	installRuntimeOTelGlobals(t, provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	var gotMember baggage.Member
	handler := newOtelHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMember = baggage.FromContext(r.Context()).Member("private-user")
		w.WriteHeader(http.StatusNoContent)
	}), "hololive.http.server")
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/private", http.NoBody)
	req.Header.Set("baggage", "private-user=private-user-123")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	if gotMember.Key() != "" {
		t.Fatalf("remote baggage reached handler context: %q", gotMember.Key())
	}
	if got := len(recorder.Ended()); got != 1 {
		t.Fatalf("ended spans = %d, want 1", got)
	}
}

func TestOtelHandlerEmptyOperationDoesNotCreateSpan(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(recorder),
	)
	installRuntimeOTelGlobals(t, provider)

	called := false
	handler := newOtelHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}), " \t ")
	handler.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/private?token=private", http.NoBody),
	)

	if !called {
		t.Fatal("handler was not called")
	}
	if got := len(recorder.Ended()); got != 0 {
		t.Fatalf("ended spans = %d, want 0", got)
	}
}

type runtimeUnsampledIDGenerator struct{}

func (runtimeUnsampledIDGenerator) NewIDs(context.Context) (trace.TraceID, trace.SpanID) {
	return trace.TraceID{
		0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	}, trace.SpanID{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
}

func (runtimeUnsampledIDGenerator) NewSpanID(context.Context, trace.TraceID) trace.SpanID {
	return trace.SpanID{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
}

func installRuntimeOTelGlobals(t *testing.T, provider *sdktrace.TracerProvider) {
	t.Helper()

	previousProvider := otel.GetTracerProvider()
	previousPropagator := otel.GetTextMapPropagator()
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Errorf("shutdown tracer provider: %v", err)
		}
	})
}
