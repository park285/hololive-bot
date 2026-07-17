package healthcheckcli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/contracts/common"
)

func TestRunNoArgsUsageError(t *testing.T) {
	var stderr strings.Builder

	code := Run(nil, &stderr)

	if code != 2 {
		t.Fatalf("Run() = %d, want 2", code)
	}
	want := "usage: healthcheck <url> [url...]|--api-key-env <env> <url> [url...]\n"
	if stderr.String() != want {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}

func TestRunHealthyURLSucceeds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var stderr strings.Builder
	code := Run([]string{server.URL, server.URL}, &stderr)

	if code != 0 {
		t.Fatalf("Run() = %d, want 0, stderr=%q", code, stderr.String())
	}
	if stderr.String() != "" {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunUnhealthyURLFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	var stderr strings.Builder
	code := Run([]string{server.URL}, &stderr)

	if code != 1 {
		t.Fatalf("Run() = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "status: 500") {
		t.Fatalf("stderr = %q, want status: 500", stderr.String())
	}
}

func TestRunAPIKeyEnvBlankNameUsageError(t *testing.T) {
	var stderr strings.Builder

	code := Run([]string{"--api-key-env", "   ", "http://127.0.0.1:1/ready"}, &stderr)

	if code != 2 {
		t.Fatalf("Run() = %d, want 2", code)
	}
	want := "api key env name must not be empty\n"
	if stderr.String() != want {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}

func TestRunAPIKeyEnvUnsetFails(t *testing.T) {
	t.Setenv("HEALTHCHECK_CLI_TEST_KEY", "")

	var stderr strings.Builder
	code := Run([]string{"--api-key-env", "HEALTHCHECK_CLI_TEST_KEY", "http://127.0.0.1:1/ready"}, &stderr)

	if code != 1 {
		t.Fatalf("Run() = %d, want 1", code)
	}
	want := "HEALTHCHECK_CLI_TEST_KEY is empty or not set\n"
	if stderr.String() != want {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}

func TestRunAPIKeyEnvSendsHeader(t *testing.T) {
	t.Setenv("HEALTHCHECK_CLI_TEST_KEY", "secret-token")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(common.APIKeyHeader) != "secret-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var stderr strings.Builder
	code := Run([]string{"--api-key-env", "HEALTHCHECK_CLI_TEST_KEY", server.URL}, &stderr)

	if code != 0 {
		t.Fatalf("Run() = %d, want 0, stderr=%q", code, stderr.String())
	}
}

func TestRunAPIKeyFlagWithoutTargetsFallsThroughToURLMode(t *testing.T) {
	var stderr strings.Builder

	code := Run([]string{"--api-key-env", "SOME_ENV"}, &stderr)

	if code != 1 {
		t.Fatalf("Run() = %d, want 1", code)
	}
	if stderr.String() == "" {
		t.Fatal("stderr empty, want url validation error")
	}
}
