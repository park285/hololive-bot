package communityshortscli

import (
	"testing"
	"time"
)

func TestParseLatencyPeriodSpecs(t *testing.T) {
	t.Parallel()

	t.Run("defaults when no flags are set", func(t *testing.T) {
		specs, err := parseLatencyPeriodSpecs(nil)
		if err != nil {
			t.Fatalf("parseLatencyPeriodSpecs() error = %v", err)
		}
		if len(specs) != 3 {
			t.Fatalf("len(specs)=%d want=3", len(specs))
		}
	})

	t.Run("parses explicit period list", func(t *testing.T) {
		specs, err := parseLatencyPeriodSpecs([]string{"last_15m=15m", "last_2h=2h"})
		if err != nil {
			t.Fatalf("parseLatencyPeriodSpecs() error = %v", err)
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
		_, err := parseLatencyPeriodSpecs([]string{"last_15m"})
		if err == nil || err.Error() != "\"last_15m\" must use label=duration" {
			t.Fatalf("parseLatencyPeriodSpecs() error = %v", err)
		}
	})

	t.Run("rejects non-positive duration", func(t *testing.T) {
		_, err := parseLatencyPeriodSpecs([]string{"last_15m=0s"})
		if err == nil || err.Error() != "\"last_15m=0s\" must be greater than zero" {
			t.Fatalf("parseLatencyPeriodSpecs() error = %v", err)
		}
	})
}
