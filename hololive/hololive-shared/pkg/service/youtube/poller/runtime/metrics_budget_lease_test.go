package polling

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func metricsTestBudgetProfile() BudgetProfile {
	return BudgetProfile{
		SourceUnits: map[BudgetSource]float64{
			BudgetSourceYouTubeScraper: 1,
		},
		BurstClass: BudgetBurstPrimary,
		Priority:   BudgetPriorityHigh,
	}
}

func TestMetricsBudgetAndLeaseCollectors(t *testing.T) {
	m, reg := newIsolatedPollerMetrics(t)
	profile := metricsTestBudgetProfile()

	m.ObserveBudgetReserve(profile, "allowed")
	m.ObserveBudgetReserve(profile, "denied")
	m.ObserveBudgetReserveWait(profile, 125*time.Millisecond)
	m.ObserveBudgetRetryAfter(profile, 3*time.Second)
	m.AddBudgetInflight(profile, 1)
	m.AddBudgetInflight(profile, -1)
	m.ObserveJobLeaseTTL("videos", 90*time.Second)
	m.ObserveJobLeaseElapsedRatio("videos", 0.8)
	m.ObserveJobLeaseNearExpiry("videos")

	families, err := reg.Gather()
	require.NoError(t, err)

	sourceLabels := map[string]string{
		"source":      string(BudgetSourceYouTubeScraper),
		"burst_class": string(BudgetBurstPrimary),
		"priority":    string(BudgetPriorityHigh),
	}
	assertCounterValue(t, families, "youtube_poller_budget_reserve_total", map[string]string{
		"source":      sourceLabels["source"],
		"result":      "allowed",
		"burst_class": sourceLabels["burst_class"],
		"priority":    sourceLabels["priority"],
	}, 1)
	assertCounterValue(t, families, "youtube_poller_budget_reserve_total", map[string]string{
		"source":      sourceLabels["source"],
		"result":      "denied",
		"burst_class": sourceLabels["burst_class"],
		"priority":    sourceLabels["priority"],
	}, 1)
	assertHistogramLabels(t, families, "youtube_poller_budget_reserve_wait_seconds", map[string]string{
		"source": sourceLabels["source"],
	})
	assertHistogramLabels(t, families, "youtube_poller_budget_retry_after_seconds", map[string]string{
		"source": sourceLabels["source"],
	})
	assertGaugeValue(t, families, "youtube_poller_budget_inflight", map[string]string{
		"source": sourceLabels["source"],
	}, 0)
	assertGaugeValue(t, families, "youtube_poller_job_lease_ttl_seconds", map[string]string{
		"poller": "videos",
	}, 90)
	assertGaugeValue(t, families, "youtube_poller_job_lease_elapsed_ratio", map[string]string{
		"poller": "videos",
	}, 0.8)
	assertCounterValue(t, families, "youtube_poller_job_lease_near_expiry_total", map[string]string{
		"poller": "videos",
	}, 1)
}
