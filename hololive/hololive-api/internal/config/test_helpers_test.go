package config

import (
	"testing"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-api/internal/config/configtest"
	"github.com/kapu/hololive-shared/pkg/config/settingstest"
)

func mustLoadAPIWorkerProfile(t *testing.T) *APIWorkerProfile {
	t.Helper()
	settingstest.UseProfileFixture(t, configtest.APIProfileFixture(t))

	profile, err := LoadAPIWorkerProfile()
	if err != nil {
		t.Fatalf("load API worker profile fixture: %v", err)
	}

	return profile
}

func testYouTubePlaneConfig(t *testing.T) YouTubePlaneConfig {
	t.Helper()

	profile := &APIWorkerProfile{Loaded: settingstest.LoadProfileFixture(t, APIWorkerIdentity(), configtest.APIProfileFixture(t))}
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
