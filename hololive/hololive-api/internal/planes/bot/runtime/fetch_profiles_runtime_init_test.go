package botruntime

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

func TestInitializeFetchProfilesRuntimeProvidesDefaultLoggerAndHTTPClient(t *testing.T) {
	runtime, cleanup, err := InitializeFetchProfilesRuntime(t.Context())
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

	if runtime.HTTPClient.Timeout != settings.DefaultOfficialProfileConfig().RequestTimeout {
		t.Fatalf(
			"InitializeFetchProfilesRuntime() timeout = %v, want %v",
			runtime.HTTPClient.Timeout,
			settings.DefaultOfficialProfileConfig().RequestTimeout,
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
