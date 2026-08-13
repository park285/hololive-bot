package internalhttp

import (
	"errors"
	"net/http"
	"testing"
)

type closableTransport struct {
	http.RoundTripper

	closed bool
	err    error
}

func (t *closableTransport) Close() error {
	t.closed = true
	return t.err
}

func TestCloseClientClosesClosableTransport(t *testing.T) {
	transport := &closableTransport{}

	if err := CloseClient(&http.Client{Transport: transport}); err != nil {
		t.Fatalf("CloseClient() error = %v", err)
	}

	if !transport.closed {
		t.Fatal("CloseClient() left the transport open; its QUIC connections outlive the owner")
	}
}

func TestCloseClientSurfacesTransportError(t *testing.T) {
	sentinel := errors.New("boom")

	err := CloseClient(&http.Client{Transport: &closableTransport{err: sentinel}})
	if !errors.Is(err, sentinel) {
		t.Fatalf("CloseClient() error = %v, want %v wrapped", err, sentinel)
	}
}

func TestCloseClientIgnoresPlainTransports(t *testing.T) {
	if err := CloseClient(nil); err != nil {
		t.Fatalf("CloseClient(nil) error = %v", err)
	}

	if err := CloseClient(&http.Client{}); err != nil {
		t.Fatalf("CloseClient(default transport) error = %v", err)
	}
}
