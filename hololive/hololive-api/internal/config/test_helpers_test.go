package config

import (
	"testing"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-shared/pkg/config/settingstest"
)

func mustLoadAPIWorkerProfile(t *testing.T) *APIWorkerProfile {
	t.Helper()
	settingstest.UseProfileFixture(t, "stack-worker-profile-api.json")

	profile, err := LoadAPIWorkerProfile()
	if err != nil {
		t.Fatalf("load API worker profile fixture: %v", err)
	}

	return profile
}

func testYouTubePlaneConfig(t *testing.T) YouTubePlaneConfig {
	t.Helper()

	profile := &APIWorkerProfile{Loaded: settingstest.LoadProfileFixture(t, "hololive", "api", "stack-worker-profile-api.json")}
	if err := workercontract.DecodeWorkerSettingsSet(profile.Loaded, map[string]any{
		WorkerBotWebhookInbox:   &profile.BotWebhookInbox,
		WorkerBotReplyOutbox:    &profile.BotReplyOutbox,
		WorkerSourceObservation: &profile.SourceObservation,
	}); err != nil {
		t.Fatal(err)
	}

	if err := validateAPIWorkerProfile(profile); err != nil {
		t.Fatal(err)
	}

	config := DefaultYouTubePlaneConfig()
	applySourceObservationWorkerProfile(&config, profile)

	return config
}
