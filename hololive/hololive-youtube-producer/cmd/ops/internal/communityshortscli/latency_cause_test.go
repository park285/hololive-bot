package communityshortscli

import (
	"testing"
	"time"
)

func TestParseLatencyCausePeriodSpecs(t *testing.T) {
	t.Parallel()

	t.Run("defaults when no flags are set", func(t *testing.T) {
		specs, err := parseLatencyCausePeriodSpecs(nil)
		if err != nil {
			t.Fatalf("parseLatencyCausePeriodSpecs() error = %v", err)
		}
		if len(specs) != 3 {
			t.Fatalf("len(specs)=%d want=3", len(specs))
		}
	})

	t.Run("parses explicit period list", func(t *testing.T) {
		specs, err := parseLatencyCausePeriodSpecs([]string{"last_15m=15m", "last_2h=2h"})
		if err != nil {
			t.Fatalf("parseLatencyCausePeriodSpecs() error = %v", err)
		}
		if len(specs) != 2 {
			t.Fatalf("len(specs)=%d want=2", len(specs))
		}
		if specs[0].Label != "last_15m" || specs[0].Window != 15*time.Minute {
			t.Fatalf("first spec=%+v", specs[0])
		}
		if specs[1].Label != "last_2h" || specs[1].Window != 2*time.Hour {
			t.Fatalf("second spec=%+v", specs[1])
		}
	})

	t.Run("rejects malformed period", func(t *testing.T) {
		_, err := parseLatencyCausePeriodSpecs([]string{"last_15m"})
		if err == nil || err.Error() != "\"last_15m\" must use label=duration" {
			t.Fatalf("parseLatencyCausePeriodSpecs() error = %v", err)
		}
	})

	t.Run("rejects non-positive duration", func(t *testing.T) {
		_, err := parseLatencyCausePeriodSpecs([]string{"last_15m=0s"})
		if err == nil || err.Error() != "\"last_15m=0s\" must be greater than zero" {
			t.Fatalf("parseLatencyCausePeriodSpecs() error = %v", err)
		}
	})
}

func TestValidateCommunityShortsLatencyCauseCLIArgs(t *testing.T) {
	t.Parallel()

	t.Run("allows recent period mode", func(t *testing.T) {
		if err := validateCommunityShortsLatencyCauseCLIArgs([]string{"last_24h=24h"}, "", ""); err != nil {
			t.Fatalf("validateCommunityShortsLatencyCauseCLIArgs() error = %v", err)
		}
	})

	t.Run("allows observation mode without periods", func(t *testing.T) {
		if err := validateCommunityShortsLatencyCauseCLIArgs(nil, "youtube-producer", "2026-04-10T00:00:00Z"); err != nil {
			t.Fatalf("validateCommunityShortsLatencyCauseCLIArgs() error = %v", err)
		}
	})

	t.Run("rejects mixed period and observation mode", func(t *testing.T) {
		err := validateCommunityShortsLatencyCauseCLIArgs([]string{"last_24h=24h"}, "youtube-producer", "2026-04-10T00:00:00Z")
		if err == nil || err.Error() != "period and observation query flags are mutually exclusive" {
			t.Fatalf("validateCommunityShortsLatencyCauseCLIArgs() error = %v", err)
		}
	})

	t.Run("rejects incomplete observation key", func(t *testing.T) {
		err := validateCommunityShortsLatencyCauseCLIArgs(nil, "youtube-producer", "")
		if err == nil || err.Error() != "observation-runtime and observation-cutover must be provided together" {
			t.Fatalf("validateCommunityShortsLatencyCauseCLIArgs() error = %v", err)
		}
	})
}
