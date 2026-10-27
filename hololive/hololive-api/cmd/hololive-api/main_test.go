package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-api/internal/planes/llm/notification"
	"github.com/kapu/hololive-shared/pkg/config/settings"
)

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

func TestHololiveAPITelemetryConfigUsesFixedIdentity(t *testing.T) {
	config := &apiconfig.RuntimeConfig{
		Bot: &apiconfig.BotPlaneConfig{Environment: "production"},
		Tracing: settings.TracingConfig{
			Enabled:    true,
			Endpoint:   "otel-collector:4317",
			Insecure:   true,
			SampleRate: 0.25,
		},
	}

	got := hololiveAPITelemetryConfig(config, "1.2.3")

	if got.ServiceName != "hololive-api" || got.ServiceVersion != "1.2.3" || got.Environment != "production" {
		t.Fatalf("telemetry identity = %#v, want fixed hololive-api identity", got)
	}

	if !got.Enabled || got.OTLPEndpoint != "otel-collector:4317" || !got.OTLPInsecure || got.SampleRate != 0.25 {
		t.Fatalf("telemetry config = %#v, want tracing settings preserved", got)
	}
}

// 수동 재실행은 kind를 그대로 넘기고, 실행 중 충돌(exit 2)과 그 밖의 실패(exit 1)를 구분하며 결과를 stdout에 남긴다.
func TestRunNotificationRerunMapsOutcomes(t *testing.T) {
	tests := []struct {
		name       string
		result     notification.Result
		err        error
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{name: "partial result", result: notification.NewResult(2, 0, 1), wantStdout: `"status":"partial"`},
		{name: "in progress", err: fmt.Errorf("rerun: %w", notification.ErrNotificationInProgress), wantCode: 2, wantStderr: "already in progress"},
		{name: "failure", err: errors.New("llm provider unavailable"), wantCode: 1, wantStderr: "llm provider unavailable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			var gotKind string

			handled, code := runNotificationRerun([]string{"--rerun-notification=majorevent-weekly"}, &stdout, &stderr, func(ctx context.Context, kind string) (notification.Result, error) {
				gotKind = kind

				if _, ok := ctx.Deadline(); !ok {
					t.Error("rerun context has no deadline")
				}

				return test.result, test.err
			})

			if !handled || code != test.wantCode || gotKind != "majorevent-weekly" {
				t.Fatalf("runNotificationRerun() = (%v, %d) kind %q, want (true, %d) kind majorevent-weekly", handled, code, gotKind, test.wantCode)
			}

			if !strings.Contains(stdout.String(), test.wantStdout) || !strings.Contains(stderr.String(), test.wantStderr) {
				t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
			}
		})
	}

	if handled, _ := runNotificationRerun([]string{"--check-config"}, io.Discard, io.Discard, nil); handled {
		t.Fatal("runNotificationRerun() handled an unrelated argument")
	}
}
