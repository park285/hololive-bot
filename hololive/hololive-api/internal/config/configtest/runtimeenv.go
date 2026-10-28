// Package configtest는 hololive-api runtime 설정 시험이 공유하는 worker profile fixture와 기동 env helper다.
package configtest

import (
	"crypto/tls"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settingstest"
)

// irisRuntimeDiagnosticsTLS는 진단 서버 인증서와 CA PEM을 프로세스당 한 번 만든다. CA 파일은 호출한 시험마다
// 그 시험의 임시 디렉터리에 다시 쓴다. 첫 시험의 디렉터리를 공유하면 그 시험이 끝난 뒤 처음 시스템 root를
// 적재하는 시험이 지워진 SSL_CERT_FILE을 읽어 인증서 검증에 실패한다.
var irisRuntimeDiagnosticsTLS = sync.OnceValues(func() (tls.Certificate, error) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()

	if len(server.TLS.Certificates) == 0 {
		return tls.Certificate{}, errors.New("test TLS server did not provide a certificate")
	}

	return server.TLS.Certificates[0], nil
})

// APIProfileFixture는 hololive-api worker identity와 짝을 이루는 profile fixture 경로다.
func APIProfileFixture(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve configtest source path")
	}

	return filepath.Join(filepath.Dir(file), "testdata", "stack-worker-profile-api.json")
}

// SetRequiredLoadEnv: bot/admin/llm 런타임 로더가 성공하는 최소 환경을 세운다.
func SetRequiredLoadEnv(t *testing.T) {
	t.Helper()
	settingstest.UseProfileFixture(t, APIProfileFixture(t))
	t.Setenv("HOLODEX_API_KEY", "test-key")
	t.Setenv("YOUTUBE_API_KEY", "test-youtube-key")
	t.Setenv("KAKAO_ROOMS", "test-room")
	t.Setenv(settingstest.IrisWebhookTokenEnv, "test-webhook-token")
	t.Setenv(settingstest.IrisBotTokenEnv, "test-bot-token")

	server := NewIrisRuntimeDiagnosticsServer(t, WorkerProfileDiagnosticsJSON())
	t.Setenv("IRIS_BASE_URL", server.URL)
	t.Setenv("IRIS_BASE_URL_ALLOWED_HOSTS", URLHostname(t, server.URL))
	t.Setenv("IRIS_TRANSPORT", "http1")
	t.Setenv("API_SECRET_KEY", "test-api-key")
	SetH3CertificateEnv(t)
}

func SetH3CertificateEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HOLOLIVE_H3_CERT_FILE", settingstest.HololiveH3CertPath)
	t.Setenv("HOLOLIVE_H3_KEY_FILE", settingstest.HololiveH3KeyPath)
}

// NewIrisRuntimeDiagnosticsServer: Iris runtime 진단 응답을 흉내내는 TLS 서버다.
func NewIrisRuntimeDiagnosticsServer(t *testing.T, body string) *httptest.Server {
	t.Helper()

	cert, err := irisRuntimeDiagnosticsTLS()
	if err != nil {
		t.Fatalf("initialize Iris diagnostics TLS failed: %v", err)
	}

	t.Setenv("SSL_CERT_FILE", writeDiagnosticsCA(t, cert))

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/diagnostics/runtime" {
			http.NotFound(w, r)

			return
		}

		w.Header().Set("Content-Type", "application/json")

		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("write diagnostics response: %v", err)
		}
	}))

	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"http/1.1"},
	}
	server.StartTLS()
	t.Cleanup(server.Close)

	return server
}

func writeDiagnosticsCA(t *testing.T, cert tls.Certificate) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "hololive-iris-diagnostics-ca.pem")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]})

	if err := os.WriteFile(path, certPEM, 0o600); err != nil {
		t.Fatalf("write Iris diagnostics CA: %v", err)
	}

	return path
}

func URLHostname(t *testing.T, raw string) string {
	t.Helper()

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse URL %q failed: %v", raw, err)
	}

	return parsed.Hostname()
}

func WorkerProfileDiagnosticsJSON() string {
	return `{
		"state": "running",
		"workers": {
			"webhook": {
				"webhookPipeline": {
					"profileEnabled": true,
					"profileVersion": 1,
					"profileId": "hololive-test",
					"profileHash": "6cfd26b9d08b5065b3aaf8d1ae320e9205962dead8753ce0ea33de7c7b140f2b",
					"workerProfile": {
						"version": 1,
						"profile_id": "hololive-test",
						"delivery": {
							"lane_workers": 32,
							"lane_queue_capacity": 128,
							"max_global_in_flight": 32,
							"max_per_endpoint_in_flight": 8,
							"max_drain_per_tick": 128,
							"max_attempts": 6,
							"request_timeout_ms": 30000,
							"lane_idle_timeout_ms": 750,
							"breaker_failure_threshold": 5,
							"breaker_cooldown_ms": 30000
						},
						"receive": {
							"workers": 16,
							"queue_size": 1000,
							"enqueue_timeout_ms": 50,
							"handler_timeout_ms": 30000,
							"max_body_bytes": 65536,
							"dedup_ttl_ms": 960000,
							"dedup_timeout_ms": 200
						},
						"validation": {
							"min_queue_per_endpoint_multiplier": 4,
							"require_receive_capacity_for_endpoint_burst": true
						}
					}
				}
			}
		}
	}`
}

func LocalStackWorkerProfileDiagnosticsJSON() string {
	return `{
		"state": "running",
		"workers": {
			"webhook": {
				"webhookPipeline": {
					"profileEnabled": true,
					"profileVersion": 1,
					"profileId": "hololive-custom-test",
					"profileHash": "081e2ddfc8d37b5399ff9d81e533b9918aec3ee3d74d1f878c71f99db4ea5855",
					"workerProfile": {
						"version": 1,
						"profile_id": "hololive-custom-test",
						"delivery": {
							"lane_workers": 32,
							"lane_queue_capacity": 128,
							"max_global_in_flight": 32,
							"max_per_endpoint_in_flight": 8,
							"max_drain_per_tick": 128,
							"max_attempts": 6,
							"request_timeout_ms": 30000,
							"lane_idle_timeout_ms": 750,
							"breaker_failure_threshold": 5,
							"breaker_cooldown_ms": 30000
						},
						"receive": {
							"workers": 20,
							"queue_size": 640,
							"enqueue_timeout_ms": 80,
							"handler_timeout_ms": 36000,
							"max_body_bytes": 262144,
							"dedup_ttl_ms": 360000,
							"dedup_timeout_ms": 300
						},
						"bot_pool": {
							"workers": 15,
							"queue_size": 200
						},
						"validation": {
							"min_queue_per_endpoint_multiplier": 4,
							"require_receive_capacity_for_endpoint_burst": true
						}
					}
				}
			}
		}
	}`
}
