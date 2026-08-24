package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type runtimeTracingCase struct {
	operation     string
	requestTarget string
	remoteAddr    string
	forwardedFor  string
	userAgent     string
}

type runtimeTracingObservation struct {
	url        *url.URL
	requestURI string
	remoteAddr string
	header     http.Header
}

func (o *runtimeTracingObservation) reset() {
	*o = runtimeTracingObservation{}
}

func (o *runtimeTracingObservation) capture(_ http.ResponseWriter, r *http.Request) {
	copiedURL := *r.URL

	o.url = &copiedURL
	o.requestURI = r.RequestURI
	o.remoteAddr = r.RemoteAddr
	o.header = r.Header.Clone()
}

func TestRuntimeHTTPServersPreserveRequestTargetAndSanitizeSpans(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(recorder),
	)
	installRuntimeOTelGlobals(t, provider)

	caseData := runtimeTracingCase{
		operation:     "hololive.http.server",
		requestTarget: "/api/private/private-user-123?token=private-token-456",
		remoteAddr:    "198.51.100.42:43123",
		forwardedFor:  "203.0.113.77, 198.51.100.42",
		userAgent:     "private-client/7.0",
	}
	observation := &runtimeTracingObservation{}

	servers := []struct {
		name    string
		handler http.Handler
	}{
		{name: "http1", handler: newRuntimeHTTP1TracingHandler(caseData.operation, observation)},
		{name: "h3", handler: newRuntimeH3TracingHandler(t, caseData.operation, observation)},
	}
	for _, server := range servers {
		t.Run(server.name, func(t *testing.T) {
			req, wantURL := newRuntimeTracingRequest(t, &caseData)

			observation.reset()
			server.handler.ServeHTTP(httptest.NewRecorder(), req)
			assertRuntimeTracingRequest(t, observation, &wantURL, &caseData)
		})
	}

	assertRuntimeTracingSpans(t, recorder.Ended(), len(servers), &caseData)
}

func newRuntimeTracingMux(observation *runtimeTracingObservation) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/private/{userID}", observation.capture)

	return mux
}

func newRuntimeHTTP1TracingHandler(operation string, observation *runtimeTracingObservation) http.Handler {
	return NewHTTPServer(":0", newRuntimeTracingMux(observation), operation).Handler
}

func newRuntimeH3TracingHandler(t *testing.T, operation string, observation *runtimeTracingObservation) http.Handler {
	t.Helper()

	certFile, keyFile := writeH3LocalhostCertificate(t)

	server, err := NewH3Server(":0", newRuntimeTracingMux(observation), certFile, keyFile, operation)
	if err != nil {
		t.Fatalf("NewH3Server() error = %v", err)
	}

	return server.Handler
}

func newRuntimeTracingRequest(t *testing.T, data *runtimeTracingCase) (*http.Request, url.URL) {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, data.requestTarget, http.NoBody)

	req.URL.RawPath = "/api/private/private-user-%31%32%33"
	req.URL.ForceQuery = true
	req.URL.Fragment = "private-fragment-789"
	req.URL.RawFragment = "private-fragment-%37%38%39"
	req.URL.Opaque = "private-opaque-abc"
	req.RemoteAddr = data.remoteAddr
	req.Header.Set("X-Forwarded-For", data.forwardedFor)
	req.Header.Set("User-Agent", data.userAgent)

	return req, *req.URL
}

func assertRuntimeTracingRequest(t *testing.T, observation *runtimeTracingObservation, wantURL *url.URL, data *runtimeTracingCase) {
	t.Helper()

	if observation.url == nil {
		t.Fatal("handler was not called")
	}

	if *observation.url != *wantURL {
		t.Fatalf("handler URL = %#v, want %#v", *observation.url, *wantURL)
	}

	if observation.requestURI != data.requestTarget {
		t.Fatalf("handler RequestURI = %q, want %q", observation.requestURI, data.requestTarget)
	}

	if observation.remoteAddr != data.remoteAddr {
		t.Fatalf("handler RemoteAddr = %q, want %q", observation.remoteAddr, data.remoteAddr)
	}

	for key, want := range map[string]string{
		"X-Forwarded-For": data.forwardedFor,
		"User-Agent":      data.userAgent,
	} {
		if got := observation.header.Get(key); got != want {
			t.Fatalf("handler header %s = %q, want %q", key, got, want)
		}
	}
}

func assertRuntimeTracingSpans(t *testing.T, spans []sdktrace.ReadOnlySpan, wantCount int, data *runtimeTracingCase) {
	t.Helper()

	if len(spans) != wantCount {
		t.Fatalf("ended spans = %d, want %d", len(spans), wantCount)
	}

	for _, span := range spans {
		assertRuntimeTracingSpan(t, span, data)
	}
}

func assertRuntimeTracingSpan(t *testing.T, span sdktrace.ReadOnlySpan, data *runtimeTracingCase) {
	t.Helper()

	if got := span.Name(); got != data.operation {
		t.Fatalf("span name = %q, want %q", got, data.operation)
	}

	forbiddenKeys := map[attribute.Key]struct{}{
		attribute.Key("client.address"):       {},
		attribute.Key("network.peer.address"): {},
		attribute.Key("network.peer.port"):    {},
		attribute.Key("user_agent.original"):  {},
	}

	for _, attr := range span.Attributes() {
		if _, forbidden := forbiddenKeys[attr.Key]; forbidden {
			t.Fatalf("forbidden client PII attribute %q recorded with value %q", attr.Key, attr.Value.String())
		}

		for _, sentinel := range runtimeTracingSentinels(data) {
			if strings.Contains(attr.Value.String(), sentinel) {
				t.Fatalf("request identifier %q recorded in span attribute %q", sentinel, attr.Key)
			}
		}
	}
}

func runtimeTracingSentinels(data *runtimeTracingCase) []string {
	return []string{
		"private-user-123",
		"private-user-%31%32%33",
		"private-token-456",
		"private-fragment-789",
		"private-fragment-%37%38%39",
		"private-opaque-abc",
		data.remoteAddr,
		"198.51.100.42",
		data.forwardedFor,
		"203.0.113.77",
		data.userAgent,
	}
}

func TestRuntimeHTTPServersBlankOperationDoesNotCreateSpan(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(recorder),
	)
	installRuntimeOTelGlobals(t, provider)

	called := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called++

		w.WriteHeader(http.StatusNoContent)
	})
	http1Server := NewHTTPServer(":0", handler, " \t\n ")
	certFile, keyFile := writeH3LocalhostCertificate(t)

	h3Server, err := NewH3Server(":0", handler, certFile, keyFile, " \t\n ")
	if err != nil {
		t.Fatalf("NewH3Server() error = %v", err)
	}

	for _, server := range []struct {
		name    string
		handler http.Handler
	}{
		{name: "http1", handler: http1Server.Handler},
		{name: "h3", handler: h3Server.Handler},
	} {
		t.Run(server.name, func(t *testing.T) {
			server.handler.ServeHTTP(
				httptest.NewRecorder(),
				httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/private?token=private", http.NoBody),
			)
		})
	}

	if called != 2 {
		t.Fatalf("handler calls = %d, want 2", called)
	}

	if got := len(recorder.Ended()); got != 0 {
		t.Fatalf("ended spans = %d, want 0", got)
	}
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

		// t.Context()는 Cleanup 실행 직전에 취소되므로, 그대로 넘기면
		// TracerProvider.Shutdown이 context canceled로 실패한다.
		if err := provider.Shutdown(context.WithoutCancel(t.Context())); err != nil {
			t.Errorf("shutdown tracer provider: %v", err)
		}
	})
}
