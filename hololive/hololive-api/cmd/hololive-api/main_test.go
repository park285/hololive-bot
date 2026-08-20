package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

func TestHololiveAPITelemetryConfigUsesFixedIdentity(t *testing.T) {
	appConfig := &settings.HololiveAPIConfig{
		Bot: &settings.Config{Environment: "production"},
		Tracing: settings.TracingConfig{
			Enabled:    true,
			Endpoint:   "otel-collector:4317",
			Insecure:   true,
			SampleRate: 0.25,
		},
	}

	got := hololiveAPITelemetryConfig(appConfig, "1.2.3")

	if got.ServiceName != "hololive-api" {
		t.Fatalf("ServiceName = %q, want hololive-api", got.ServiceName)
	}
	if got.ServiceVersion != "1.2.3" {
		t.Fatalf("ServiceVersion = %q, want 1.2.3", got.ServiceVersion)
	}
	if got.Environment != "production" {
		t.Fatalf("Environment = %q, want production", got.Environment)
	}
	if !got.Enabled || got.OTLPEndpoint != "otel-collector:4317" || !got.OTLPInsecure || got.SampleRate != 0.25 {
		t.Fatalf("telemetry config = %#v, want tracing settings preserved", got)
	}
}

func TestRunConfigCheck(t *testing.T) {
	tests := []struct {
		name       string
		loadErr    error
		wantCode   int
		wantOutput string
	}{
		{name: "valid", wantOutput: "hololive-api config valid"},
		{name: "invalid", loadErr: errors.New("retention policy is invalid"), wantCode: 1, wantOutput: "retention policy is invalid"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stderr bytes.Buffer
			handled, code := runConfigCheck([]string{"--check-config"}, &stderr, func() error {
				return test.loadErr
			})
			if !handled {
				t.Fatal("runConfigCheck() did not handle --check-config")
			}
			if code != test.wantCode {
				t.Fatalf("runConfigCheck() code = %d, want %d", code, test.wantCode)
			}
			if !strings.Contains(stderr.String(), test.wantOutput) {
				t.Fatalf("runConfigCheck() output = %q, want substring %q", stderr.String(), test.wantOutput)
			}
		})
	}
}

func TestRunConfigCheckIgnoresStartupArguments(t *testing.T) {
	called := false
	handled, code := runConfigCheck(nil, &bytes.Buffer{}, func() error {
		called = true
		return nil
	})
	if handled || code != 0 {
		t.Fatalf("runConfigCheck() = (%t, %d), want (false, 0)", handled, code)
	}
	if called {
		t.Fatal("runConfigCheck() called loader for ordinary startup")
	}
}

func TestRunWorkerProfileCheck(t *testing.T) {
	var stderr bytes.Buffer
	handled, code := runWorkerProfileCheck([]string{"--check-worker-profile"}, &stderr, func() error { return nil })
	if !handled || code != 0 || !strings.Contains(stderr.String(), "worker profile valid") {
		t.Fatalf("runWorkerProfileCheck() = (%t,%d,%q)", handled, code, stderr.String())
	}
	stderr.Reset()
	handled, code = runWorkerProfileCheck([]string{"--check-worker-profile"}, &stderr, func() error { return errors.New("invalid profile") })
	if !handled || code != 1 || !strings.Contains(stderr.String(), "invalid profile") {
		t.Fatalf("runWorkerProfileCheck() failure = (%t,%d,%q)", handled, code, stderr.String())
	}
}
