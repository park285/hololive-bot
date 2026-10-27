package botruntime

import (
	"log/slog"
	"testing"
	"time"

	"github.com/kapu/hololive-api/internal/config/configtest"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-shared/pkg/config/settingstest"
	"github.com/kapu/hololive-shared/pkg/constants"
)

func TestDurableConfiguredSettlementTimeoutLeavesSharedShutdownHeadroom(t *testing.T) {
	settingstest.UseProfileFixture(t, configtest.APIProfileFixture(t))

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

func testDurableProfile(t *testing.T) *apiconfig.APIWorkerProfile {
	t.Helper()

	profile := &apiconfig.APIWorkerProfile{Loaded: settingstest.LoadProfileFixture(t, apiconfig.APIWorkerIdentity(), configtest.APIProfileFixture(t))}
	if err := workercontract.DecodeWorkerSettingsSet(profile.Loaded, map[string]any{
		apiconfig.WorkerBotWebhookInbox:   &profile.BotWebhookInbox,
		apiconfig.WorkerBotReplyOutbox:    &profile.BotReplyOutbox,
		apiconfig.WorkerSourceObservation: &profile.SourceObservation,
	}); err != nil {
		t.Fatal(err)
	}

	return profile
}

func TestDurableReplyUsesExecutorAttemptTimeout(t *testing.T) {
	profile := testDurableProfile(t)
	worker := profile.Loaded.Profile.Workers[apiconfig.WorkerBotReplyOutbox]

	worker.Executor.AttemptTimeout.Milliseconds = new(int64(42000))
	profile.Loaded.Profile.Workers[apiconfig.WorkerBotReplyOutbox] = worker

	runtime, err := newDurableRuntime(nil, nil, nil, profile, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}

	if runtime.dispatchBudget != 42*time.Second {
		t.Fatalf("reply attempt budget = %s", runtime.dispatchBudget)
	}
}
