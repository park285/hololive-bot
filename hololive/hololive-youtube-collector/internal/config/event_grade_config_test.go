package config

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

func TestEventGradeCollectionIsReservedForCollectorC(t *testing.T) {
	holodex := settings.DefaultHolodexOperationalConfig().Timeout
	official := settings.DefaultOfficialScheduleConfig().Timeout

	for _, tc := range []struct {
		name     string
		instance string
		enabled  bool
		secret   string
		ok       bool
	}{
		{"c enabled", collectorInstanceC, true, "/run/hololive-bot/event-grade/secret.json", true},
		{"disabled without secret", "youtube-collector-b", false, "", true},
		{"other slot enabled", "youtube-collector-b", true, "/run/hololive-bot/event-grade/secret.json", false},
		{"other slot secret only", "youtube-collector-d", false, "/run/hololive-bot/event-grade/secret.json", false},
		{"c secret without profile", collectorInstanceC, false, "/run/hololive-bot/event-grade/secret.json", false},
		{"c relative secret", collectorInstanceC, true, "secret.json", false},
		{"c missing secret", collectorInstanceC, true, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := fixtureCollectorConfig(t)

			cfg.InstanceID = tc.instance
			cfg.EventGradeEnabled = tc.enabled
			cfg.EventGradeSecretFile = tc.secret

			err := cfg.Validate(holodex, official)
			if tc.ok && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}

			if !tc.ok && err == nil {
				t.Fatal("Validate() = nil, want event grade rejection")
			}
		})
	}
}
