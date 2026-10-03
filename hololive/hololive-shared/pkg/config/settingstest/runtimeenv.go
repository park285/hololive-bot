package settingstest

import (
	"crypto/tls"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"
)

var (
	irisRuntimeDiagnosticsTLSOnce     sync.Once
	irisRuntimeDiagnosticsTLSCert     tls.Certificate
	irisRuntimeDiagnosticsTLSCertFile string
	errIrisRuntimeDiagnosticsTLS      error
)

// SetRequiredLoadEnv: bot/admin/llm 런타임 로더가 성공하는 최소 환경을 세운다.
func SetRequiredLoadEnv(t *testing.T) {
	t.Helper()
	UseProfileFixture(t, "stack-worker-profile-api.json")
	t.Setenv("HOLODEX_API_KEY", "test-key")
	t.Setenv("YOUTUBE_API_KEY", "test-youtube-key")
	t.Setenv("KAKAO_ROOMS", "test-room")
	t.Setenv(IrisWebhookTokenEnv, "test-webhook-token")
	t.Setenv(IrisBotTokenEnv, "test-bot-token")

	server := NewIrisRuntimeDiagnosticsServer(t, WorkerProfileDiagnosticsJSON())
	t.Setenv("IRIS_BASE_URL", server.URL)
	t.Setenv("IRIS_BASE_URL_ALLOWED_HOSTS", URLHostname(t, server.URL))
	t.Setenv("IRIS_TRANSPORT", "http1")
	t.Setenv("API_SECRET_KEY", "test-api-key")
	SetH3CertificateEnv(t)
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://admin.example.com")
}

func SetH3CertificateEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HOLOLIVE_H3_CERT_FILE", HololiveH3CertPath)
	t.Setenv("HOLOLIVE_H3_KEY_FILE", HololiveH3KeyPath)
}

// NewIrisRuntimeDiagnosticsServer: Iris runtime 진단 응답을 흉내내는 TLS 서버다.
func NewIrisRuntimeDiagnosticsServer(t *testing.T, body string) *httptest.Server {
	t.Helper()

	cert, certFile := irisRuntimeDiagnosticsTLS(t)
	t.Setenv("SSL_CERT_FILE", certFile)

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

func irisRuntimeDiagnosticsTLS(t *testing.T) (cert tls.Certificate, certFile string) {
	t.Helper()

	irisRuntimeDiagnosticsTLSOnce.Do(func() {
		server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		defer server.Close()

		if len(server.TLS.Certificates) == 0 {
			errIrisRuntimeDiagnosticsTLS = errors.New("test TLS server did not provide a certificate")
			return
		}

		certPEM := pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: server.Certificate().Raw,
		})

		file, err := os.CreateTemp(t.TempDir(), "hololive-iris-diagnostics-ca-*.pem")
		if err != nil {
			errIrisRuntimeDiagnosticsTLS = err
			return
		}

		if _, err := file.Write(certPEM); err != nil {
			errIrisRuntimeDiagnosticsTLS = err

			if closeErr := file.Close(); closeErr != nil {
				errIrisRuntimeDiagnosticsTLS = errors.Join(errIrisRuntimeDiagnosticsTLS, closeErr)
			}

			return
		}

		if err := file.Close(); err != nil {
			errIrisRuntimeDiagnosticsTLS = err
			return
		}

		irisRuntimeDiagnosticsTLSCert = server.TLS.Certificates[0]
		irisRuntimeDiagnosticsTLSCertFile = file.Name()
	})

	if errIrisRuntimeDiagnosticsTLS != nil {
		t.Fatalf("initialize Iris diagnostics TLS failed: %v", errIrisRuntimeDiagnosticsTLS)
	}

	return irisRuntimeDiagnosticsTLSCert, irisRuntimeDiagnosticsTLSCertFile
}

func URLHostname(t *testing.T, raw string) string {
	t.Helper()

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse URL %q failed: %v", raw, err)
	}

	return parsed.Hostname()
}

func WriteIrisBaseURLFile(t *testing.T, raw string) string {
	t.Helper()

	path := t.TempDir() + "/iris_base_url"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write IRIS_BASE_URL_FILE failed: %v", err)
	}

	return path
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
