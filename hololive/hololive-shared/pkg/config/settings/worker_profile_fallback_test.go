package settings

import (
	"testing"
	"time"

	"github.com/park285/shared-go/pkg/workerconfig"
)

func TestResolveIrisBotWebhookWorkerProfileSkipsFetchWhenDisabled(t *testing.T) {
	config := IrisConfig{BotToken: "test-token", BaseURL: "https://127.0.0.1:1"}

	got := resolveIrisBotWebhookWorkerProfile(&config, configLoadOptions{})

	want := workerconfig.DefaultIrisBotWebhookWorkerProfile()
	if got.ProfileHash() != want.ProfileHash() {
		t.Fatalf("profile hash = %q, want default %q", got.ProfileHash(), want.ProfileHash())
	}
}

// Iris 진단 조회가 계속 실패해도 기동은 기본 프로파일로 계속돼야 한다(crash-loop 결합 차단).
func TestResolveIrisBotWebhookWorkerProfileFallsBackWhenFetchKeepsFailing(t *testing.T) {
	server := newIrisRuntimeDiagnosticsServer(t, `{"workers":{}}`)
	t.Setenv("IRIS_TRANSPORT", "http1")
	t.Setenv("IRIS_BASE_URL_ALLOWED_HOSTS", testURLHostname(t, server.URL))
	config := IrisConfig{
		BaseURL:                   server.URL,
		BotToken:                  "test-token",
		HTTPTimeout:               time.Second,
		HTTPDialTimeout:           time.Second,
		HTTPResponseHeaderTimeout: time.Second,
	}

	if _, err := fetchIrisBotWebhookWorkerProfile(&config); err == nil {
		t.Fatal("test precondition: fetchIrisBotWebhookWorkerProfile() must fail for this payload")
	}

	got := resolveIrisBotWebhookWorkerProfile(&config, configLoadOptions{FetchIrisWorkerProfile: true})

	want := workerconfig.DefaultIrisBotWebhookWorkerProfile()
	if got.ProfileHash() != want.ProfileHash() {
		t.Fatalf("profile hash = %q, want default %q", got.ProfileHash(), want.ProfileHash())
	}
	if got.Version != want.Version {
		t.Fatalf("profile version = %d, want default %d", got.Version, want.Version)
	}
}

func TestFetchIrisBotWebhookWorkerProfileWithRetryStopsOnDisabled(t *testing.T) {
	config := IrisConfig{BotToken: "   "}

	start := time.Now()
	_, err := fetchIrisBotWebhookWorkerProfileWithRetry(&config)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("fetchIrisBotWebhookWorkerProfileWithRetry() error = nil, want disabled error")
	}
	if elapsed >= irisWorkerProfileRetryBaseDelay {
		t.Fatalf("elapsed = %s, want no backoff sleep for a disabled profile", elapsed)
	}
}
