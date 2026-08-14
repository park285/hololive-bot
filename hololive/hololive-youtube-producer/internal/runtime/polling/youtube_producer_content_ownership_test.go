package polling

import (
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

func TestProductionRegistrationsOmitVideosShortsAndShortsBackfill(t *testing.T) {
	t.Parallel()
	configs := []settings.ScraperBackfillConfig{
		{},
		{
			Enabled:        true,
			ShortsEnabled:  true,
			ShortsInterval: 5 * time.Minute,
			LiveEnabled:    true,
			LiveInterval:   3 * time.Minute,
		},
	}
	for _, backfill := range configs {
		registrations := buildBackfillTestRegistrations(backfill, []string{"UC_A"})
		for _, registration := range registrations {
			name := registration.Poller.Name()
			switch name {
			case "videos", "shorts", "shorts_backfill", "live", "live_batch", "live_backfill", "live_backfill_batch", "channel_stats":
				t.Fatalf("production-reachable producer wiring registered %q", name)
			}
		}
	}
}
