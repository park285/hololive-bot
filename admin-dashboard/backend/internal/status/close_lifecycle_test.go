package status

import (
	"net/http"
	"sync/atomic"
	"testing"
)

type countingTransport struct {
	closes atomic.Int64
}

func (t *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, http.ErrServerClosed
}

func (t *countingTransport) Close() error {
	t.closes.Add(1)

	return nil
}

func newCountingSampler(t *testing.T) (*Sampler, *countingTransport) {
	t.Helper()

	transport := &countingTransport{}

	return &Sampler{
		endpoints: []ServiceEndpoint{{Name: "probe", URL: "https://probe.invalid", HealthPath: testHealthPath}},
		clients: map[string]endpointClient{
			"probe": {client: &http.Client{Transport: transport}},
		},
	}, transport
}

func TestSamplerCloseIsIdempotent(t *testing.T) {
	t.Parallel()

	sampler, transport := newCountingSampler(t)

	for range 3 {
		if err := sampler.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}

	if got := transport.closes.Load(); got != 1 {
		t.Fatalf("transport closed %d times, want exactly 1", got)
	}
}

func TestCollectorDoesNotCloseABorrowedSampler(t *testing.T) {
	t.Parallel()

	sampler, transport := newCountingSampler(t)

	if err := NewCollectorWithSampler(sampler, "v-test").Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if got := transport.closes.Load(); got != 0 {
		t.Fatalf("borrowed sampler closed %d times, want 0; its owner still uses it", got)
	}

	if err := sampler.Close(); err != nil {
		t.Fatalf("owner Close() error = %v", err)
	}

	if got := transport.closes.Load(); got != 1 {
		t.Fatalf("owner close produced %d transport closes, want 1", got)
	}
}

func TestCollectorClosesTheSamplerItCreated(t *testing.T) {
	t.Parallel()

	collector := NewCollector([]ServiceEndpoint{{Name: "probe", URL: "https://probe.invalid", HealthPath: testHealthPath}}, "v-test")

	transport := &countingTransport{}

	collector.sampler.clients = map[string]endpointClient{"probe": {client: &http.Client{Transport: transport}}}

	if err := collector.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if got := transport.closes.Load(); got != 1 {
		t.Fatalf("owned sampler closed %d times, want 1", got)
	}
}
