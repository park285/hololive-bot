package botruntime

import (
	"log/slog"
	"testing"
	"time"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-shared/pkg/config/settingstest"
	"github.com/kapu/hololive-shared/pkg/constants"
)

func TestDurableConfiguredSettlementTimeoutLeavesSharedShutdownHeadroom(t *testing.T) {
	settingstest.UseProfileFixture(t, "stack-worker-profile-api.json")

	profile, err := apiconfig.LoadAPIWorkerProfile()
	if err != nil {
		t.Fatal(err)
	}

	runtime, err := newDurableRuntime(nil, nil, nil, profile, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}

	want := time.Duration(profile.BotWebhookInbox.SettlementTimeoutMS) * time.Millisecond
	if runtime.settlementTimeout != want {
		t.Fatalf("runtime settlement timeout=%s want configured=%s", runtime.settlementTimeout, want)
	}

	if runtime.settlementTimeout <= 0 || runtime.settlementTimeout > constants.AppTimeout.Shutdown/2 {
		t.Fatalf("configured runtime settlement timeout=%s leaves too little shared shutdown budget=%s", runtime.settlementTimeout, constants.AppTimeout.Shutdown)
	}
}
