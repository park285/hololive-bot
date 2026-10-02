package collectorruntime

import (
	"log/slog"
	"testing"

	collectorconfig "github.com/kapu/hololive-shared/pkg/config/settings/collector"
)

func TestBuildRequiresRuntimeAllowEnv(t *testing.T) {
	runtime, err := Build(t.Context(), &collectorconfig.RuntimeConfig{
		RuntimeOwnership: collectorconfig.RuntimeOwnershipConfig{},
	}, testLogger())
	if err == nil || runtime != nil {
		t.Fatalf("Build() = %#v, %v, want runtime disabled error", runtime, err)
	}

	if err.Error() != "youtube collector runtime disabled: set YOUTUBE_COLLECTOR_RUNTIME_ALLOWED=true on the owning host" {
		t.Fatalf("Build() error = %q", err)
	}
}

func TestBuildRequiresWorkerProfile(t *testing.T) {
	runtime, err := Build(t.Context(), &collectorconfig.RuntimeConfig{
		RuntimeOwnership: collectorconfig.RuntimeOwnershipConfig{
			RuntimeAllowed: true,
		},
	}, testLogger())
	if err == nil || runtime != nil {
		t.Fatalf("Build() = %#v, %v, want worker profile error", runtime, err)
	}

	if err.Error() != "youtube collector worker profile is required" {
		t.Fatalf("Build() error = %q", err)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}
