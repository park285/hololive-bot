package configtest

import (
	"net/http"
	"testing"
)

// 첫 시험이 TLS 검증 없이 끝나 그 시험의 임시 디렉터리가 지워진 뒤에도, 나중 시험의 첫 시스템 root 적재가
// 진단 서버 CA를 읽어야 한다. Go는 시스템 root를 프로세스당 한 번 SSL_CERT_FILE에서 적재한다.
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
