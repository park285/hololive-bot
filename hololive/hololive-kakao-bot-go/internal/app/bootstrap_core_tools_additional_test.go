package app

import (
	"context"
	"testing"

	"github.com/kapu/hololive-shared/pkg/constants"
)

func TestInitializeFetchProfilesRuntimeProvidesDefaultLoggerAndHTTPClient(t *testing.T) {
	runtime, cleanup, err := InitializeFetchProfilesRuntime(context.Background())
	if err != nil {
		t.Fatalf("InitializeFetchProfilesRuntime() error = %v, want nil", err)
	}
	if runtime == nil {
		t.Fatal("InitializeFetchProfilesRuntime() runtime = nil")
	}
	if runtime.Logger == nil {
		t.Fatal("InitializeFetchProfilesRuntime() logger = nil")
	}
	if runtime.HTTPClient == nil {
		t.Fatal("InitializeFetchProfilesRuntime() http client = nil")
	}
	if runtime.HTTPClient.Timeout != constants.OfficialProfileConfig.RequestTimeout {
		t.Fatalf(
			"InitializeFetchProfilesRuntime() timeout = %v, want %v",
			runtime.HTTPClient.Timeout,
			constants.OfficialProfileConfig.RequestTimeout,
		)
	}
	if runtime.HTTPClient.Transport == nil {
		t.Fatal("InitializeFetchProfilesRuntime() transport = nil")
	}
	if cleanup == nil {
		t.Fatal("InitializeFetchProfilesRuntime() cleanup = nil")
	}

	cleanup()
}
