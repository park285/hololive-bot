package configtest

import (
	"net/http"
	"testing"
)

func TestDiagnosticsServerCAOutlivesEarlierTest(t *testing.T) {
	t.Run("first test without TLS verification", func(t *testing.T) {
		NewIrisRuntimeDiagnosticsServer(t, WorkerProfileDiagnosticsJSON())
	})

	t.Run("later test verifies TLS", func(t *testing.T) {
		server := NewIrisRuntimeDiagnosticsServer(t, WorkerProfileDiagnosticsJSON())

		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/diagnostics/runtime", http.NoBody)
		if err != nil {
			t.Fatal(err)
		}

		client := &http.Client{Transport: &http.Transport{}}

		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("diagnostics request with system roots: %v", err)
		}

		if err := response.Body.Close(); err != nil {
			t.Fatal(err)
		}

		if response.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", response.StatusCode)
		}
	})
}
