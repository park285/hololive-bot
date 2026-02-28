package majorevent

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
)

func newTestScraperScheduler() *ScraperScheduler {
	return &ScraperScheduler{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestScraperScheduler_buildRetryRuns(t *testing.T) {
	scheduler := newTestScraperScheduler()
	regularRun := time.Date(2026, 2, 10, 6, 0, 0, 0, kst)

	tests := []struct {
		name     string
		failedAt time.Time
		expected []time.Time
	}{
		{
			name:     "regular failure queues configured same-day retries",
			failedAt: regularRun.Add(5 * time.Minute),
			expected: []time.Time{
				time.Date(2026, 2, 10, 6, 30, 0, 0, kst),
				time.Date(2026, 2, 10, 8, 0, 0, 0, kst),
				time.Date(2026, 2, 10, 12, 0, 0, 0, kst),
			},
		},
		{
			name:     "past retry slots are skipped",
			failedAt: regularRun.Add(2*time.Hour + 30*time.Minute),
			expected: []time.Time{
				time.Date(2026, 2, 10, 12, 0, 0, 0, kst),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scheduler.buildRetryRuns(regularRun, tt.failedAt)
			if len(got) != len(tt.expected) {
				t.Fatalf("len(got)=%d, want %d", len(got), len(tt.expected))
			}
			for i := range tt.expected {
				if !got[i].Equal(tt.expected[i]) {
					t.Errorf("got[%d]=%v, want %v", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestScraperScheduler_handleScrapeResult_clearsRetryQueueOnSuccess(t *testing.T) {
	scheduler := newTestScraperScheduler()
	scheduler.setRetryRuns([]time.Time{
		time.Date(2026, 2, 10, 8, 0, 0, 0, kst),
		time.Date(2026, 2, 10, 12, 0, 0, 0, kst),
	})

	scheduler.handleScrapeResult(
		scrapeTriggerRetry,
		time.Date(2026, 2, 10, 8, 0, 0, 0, kst),
		time.Date(2026, 2, 10, 8, 1, 0, 0, kst),
		nil,
	)

	if count := scheduler.retryRunCount(); count != 0 {
		t.Fatalf("retryRunCount=%d, want 0", count)
	}
}

func TestScraperScheduler_calculateNextRun_prefersRetryThenFallsBackToNextDay(t *testing.T) {
	scheduler := newTestScraperScheduler()
	now := time.Date(2026, 2, 10, 7, 0, 0, 0, kst)
	retryRun := time.Date(2026, 2, 10, 8, 0, 0, 0, kst)
	scheduler.setRetryRuns([]time.Time{retryRun})

	next := scheduler.calculateNextRun(now)
	if !next.Equal(retryRun) {
		t.Fatalf("next=%v, want retry run %v", next, retryRun)
	}

	scheduler.clearRetryRuns()
	next = scheduler.calculateNextRun(time.Date(2026, 2, 10, 12, 0, 0, 0, kst))
	expectedRegular := time.Date(2026, 2, 11, 6, 0, 0, 0, kst)
	if !next.Equal(expectedRegular) {
		t.Fatalf("next=%v, want next regular run %v", next, expectedRegular)
	}
}

func TestScraperScheduler_ScrapeHourKST_DefaultAndOverride(t *testing.T) {
	defaultScheduler := NewScraperScheduler(nil, nil, nil, testLogger())
	if got, want := defaultScheduler.ScrapeHourKST(), constants.MajorEventConfig.ScrapeHourKST; got != want {
		t.Fatalf("default scrape hour = %d, want %d", got, want)
	}

	customScheduler := NewScraperScheduler(
		nil,
		nil,
		nil,
		testLogger(),
		WithScraperSchedulerHour(4),
	)
	if got, want := customScheduler.ScrapeHourKST(), 4; got != want {
		t.Fatalf("custom scrape hour = %d, want %d", got, want)
	}

	clampedScheduler := NewScraperScheduler(
		nil,
		nil,
		nil,
		testLogger(),
		WithScraperSchedulerHour(25),
	)
	if got, want := clampedScheduler.ScrapeHourKST(), 23; got != want {
		t.Fatalf("clamped scrape hour = %d, want %d", got, want)
	}
}

func TestScraperScheduler_SetScrapeHourKST(t *testing.T) {
	scheduler := NewScraperScheduler(nil, nil, nil, testLogger(), WithScraperSchedulerHour(5))

	applied := scheduler.SetScrapeHourKST(9)
	if applied != 9 {
		t.Fatalf("applied = %d, want %d", applied, 9)
	}
	if got := scheduler.ScrapeHourKST(); got != 9 {
		t.Fatalf("scrape hour after set = %d, want %d", got, 9)
	}

	applied = scheduler.SetScrapeHourKST(99)
	if applied != 23 {
		t.Fatalf("applied = %d, want %d", applied, 23)
	}
	if got := scheduler.ScrapeHourKST(); got != 23 {
		t.Fatalf("scrape hour after clamp = %d, want %d", got, 23)
	}
}
