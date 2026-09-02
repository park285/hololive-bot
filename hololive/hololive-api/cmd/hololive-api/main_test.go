package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/config/settings/apiplane"
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

func TestRunHololiveAPIInitializesAndRunsFxApplication(t *testing.T) {
	config := mainTestConfig()
	closer := &mainTestCloser{}
	application := &mainTestApplication{}
	initializedVersion := ""

	var capturedCloser io.Closer

	applicationBuilt := false
	dependencies := startupDependencies{
		initialize: func(version string) { initializedVersion = version },
		loadConfig: func() (*apiplane.RuntimeConfig, error) {
			return config, nil
		},
		newLogger: func(got *apiplane.RuntimeConfig) (loggerResult, error) {
			if got != config {
				t.Fatal("newLogger() received a different config")
			}

			return loggerResult{logger: slog.New(slog.DiscardHandler), closer: closer}, nil
		},
		newApplication: func(
			ctx context.Context,
			got *apiplane.RuntimeConfig,
			_ *slog.Logger,
			version string,
		) (hololiveAPIApplication, error) {
			applicationBuilt = true

			if got != config || version != Version {
				t.Fatalf("newApplication() config/version = (%p,%q), want (%p,%q)", got, version, config, Version)
			}

			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("newApplication() build context has no deadline")
			}

			return application, nil
		},
		stderr: io.Discard,
	}

	code := runHololiveAPIWithDependencies(func(value io.Closer) { capturedCloser = value }, dependencies)

	if code != 0 || !applicationBuilt || application.runCalls != 1 {
		t.Fatalf("run result = (code=%d,built=%t,runs=%d)", code, applicationBuilt, application.runCalls)
	}

	if initializedVersion != Version {
		t.Fatalf("initialized version = %q, want %q", initializedVersion, Version)
	}

	if capturedCloser != closer {
		t.Fatal("logger closer was not retained for main")
	}
}

func TestRunHololiveAPIStopsBeforeLoggerAndFxOnConfigFailure(t *testing.T) {
	var stderr bytes.Buffer

	loggerCalled := false
	applicationCalled := false
	dependencies := startupDependencies{
		initialize: func(string) {},
		loadConfig: func() (*apiplane.RuntimeConfig, error) {
			return nil, errors.New("postgres://user:canary-secret@db:5432/app")
		},
		newLogger: func(*apiplane.RuntimeConfig) (loggerResult, error) {
			loggerCalled = true

			return loggerResult{}, errors.New("newLogger must not be called")
		},
		newApplication: func(context.Context, *apiplane.RuntimeConfig, *slog.Logger, string) (hololiveAPIApplication, error) {
			applicationCalled = true

			return nil, errors.New("newApplication must not be called")
		},
		stderr: &stderr,
	}

	code := runHololiveAPIWithDependencies(nil, dependencies)

	if code != 1 || loggerCalled || applicationCalled {
		t.Fatalf("run result = (code=%d,logger=%t,application=%t)", code, loggerCalled, applicationCalled)
	}

	if strings.Contains(stderr.String(), "canary-secret") {
		t.Fatalf("config diagnostic leaked credential: %q", stderr.String())
	}
}

func TestRunHololiveAPIReturnsOneAfterFxConstructionFailure(t *testing.T) {
	var logs bytes.Buffer

	buildErr := errors.New("Fx graph failed")
	dependencies := startupDependencies{
		initialize: func(string) {},
		loadConfig: func() (*apiplane.RuntimeConfig, error) {
			return mainTestConfig(), nil
		},
		newLogger: func(*apiplane.RuntimeConfig) (loggerResult, error) {
			return loggerResult{
				logger: slog.New(slog.NewTextHandler(&logs, nil)),
				closer: &mainTestCloser{},
			}, nil
		},
		newApplication: func(context.Context, *apiplane.RuntimeConfig, *slog.Logger, string) (hololiveAPIApplication, error) {
			return nil, buildErr
		},
		stderr: io.Discard,
	}

	if code := runHololiveAPIWithDependencies(func(io.Closer) {}, dependencies); code != 1 {
		t.Fatalf("runHololiveAPIWithDependencies() = %d, want 1", code)
	}

	if !strings.Contains(logs.String(), "Failed to assemble hololive-api runtime") {
		t.Fatalf("logs = %q, want Fx construction diagnostic", logs.String())
	}
}

type mainTestApplication struct {
	runCalls int
}

func (a *mainTestApplication) Run(*slog.Logger) int {
	a.runCalls++

	return 0
}

type mainTestCloser struct{}

func (*mainTestCloser) Close() error {
	return nil
}

func mainTestConfig() *apiplane.RuntimeConfig {
	return &apiplane.RuntimeConfig{
		Bot:   &settings.Config{Server: settings.ServerConfig{Port: 30001}},
		Admin: &settings.Config{Server: settings.ServerConfig{Port: 30006}},
		LLM:   &apiplane.LLMSchedulerConfig{Server: settings.ServerConfig{Port: 30003}},
		Logging: settings.LoggingConfig{
			Level: "info",
		},
	}
}
