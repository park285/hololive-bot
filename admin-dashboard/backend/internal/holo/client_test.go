package holo

import (
	"path/filepath"
	"testing"

	"github.com/quic-go/quic-go/http3"
)

func TestNewClientReturnsErrorWhenH3ClientConfigFails(t *testing.T) {
	t.Setenv("HOLOLIVE_INTERNAL_H3_CA_CERT_FILE", filepath.Join(t.TempDir(), "missing-ca.pem"))

	client, err := NewClient("https://hololive-admin-api:30006", "test-key")
	if err == nil {
		t.Fatal("NewClient() error = nil, want h3 client config error")
	}
	if client != nil {
		t.Fatalf("NewClient() = %v, want nil on h3 config error", client)
	}
}

func TestNewClientTransportSelection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
		wantH3  bool
	}{
		{name: "https uses h3 transport", baseURL: "https://hololive-admin-api:30006", wantH3: true},
		{name: "http keeps tcp transport", baseURL: "http://hololive-admin-api:30006", wantH3: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, err := NewClient(tt.baseURL, "test-key")
			if err != nil {
				t.Fatalf("NewClient(%q) error = %v", tt.baseURL, err)
			}

			_, isH3 := client.http.Transport.(*http3.Transport)
			if isH3 != tt.wantH3 {
				t.Fatalf("NewClient(%q) transport h3 = %v, want %v (transport %T)", tt.baseURL, isH3, tt.wantH3, client.http.Transport)
			}
		})
	}
}
