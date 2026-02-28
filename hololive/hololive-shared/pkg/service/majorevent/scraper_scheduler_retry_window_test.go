package majorevent

import (
	"errors"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
)

func TestScraperScheduler_BuildRetryRuns_UsesSameDayFutureWindow(t *testing.T) {
	orig := append([]time.Duration(nil), constants.MajorEventConfig.ScrapeRetryDelays...)
	defer func() { constants.MajorEventConfig.ScrapeRetryDelays = orig }()

	constants.MajorEventConfig.ScrapeRetryDelays = []time.Duration{
		30 * time.Minute,
		2 * time.Hour,
		20 * time.Hour,
		-1 * time.Minute,
	}

	scheduler := &ScraperScheduler{logger: testLogger()}
	baseRun := time.Date(2026, 2, 10, 6, 0, 0, 0, kst)
	failedAt := baseRun.Add(45 * time.Minute)

	retries := scheduler.buildRetryRuns(baseRun, failedAt)
	if len(retries) != 1 {
		t.Fatalf("expected 1 retry run, got %d", len(retries))
	}

	want := baseRun.Add(2 * time.Hour).In(kst)
	if !retries[0].Equal(want) {
		t.Fatalf("expected retry run %v, got %v", want, retries[0])
	}
}

func TestScraperScheduler_RetryWindow_CalculatesAndConsumesOnFailure(t *testing.T) {
	orig := append([]time.Duration(nil), constants.MajorEventConfig.ScrapeRetryDelays...)
	defer func() { constants.MajorEventConfig.ScrapeRetryDelays = orig }()

	constants.MajorEventConfig.ScrapeRetryDelays = []time.Duration{
		30 * time.Minute,
		2 * time.Hour,
	}

	scheduler := &ScraperScheduler{logger: testLogger()}
	scheduledAt := time.Date(2026, 2, 10, 6, 0, 0, 0, kst)
	failedAt := scheduledAt.Add(5 * time.Minute)

	scheduler.handleScrapeResult(scrapeTriggerRegular, scheduledAt, failedAt, errors.New("scrape failed"))

	if got := scheduler.retryRunCount(); got != 2 {
		t.Fatalf("expected 2 queued retry runs, got %d", got)
	}

	firstRetry := scheduledAt.Add(30 * time.Minute).In(kst)
	next, trigger := scheduler.nextRunWithTrigger(scheduledAt.Add(6 * time.Minute))
	if trigger != scrapeTriggerRetry {
		t.Fatalf("expected retry trigger, got %s", trigger)
	}
	if !next.Equal(firstRetry) {
		t.Fatalf("expected first retry %v, got %v", firstRetry, next)
	}

	popped, ok := scheduler.popNextRetryRun()
	if !ok {
		t.Fatal("expected first retry to be consumable")
	}
	if !popped.Equal(firstRetry) {
		t.Fatalf("expected popped retry %v, got %v", firstRetry, popped)
	}

	secondRetry := scheduledAt.Add(2 * time.Hour).In(kst)
	next, trigger = scheduler.nextRunWithTrigger(firstRetry.Add(1 * time.Minute))
	if trigger != scrapeTriggerRetry {
		t.Fatalf("expected retry trigger for second retry, got %s", trigger)
	}
	if !next.Equal(secondRetry) {
		t.Fatalf("expected second retry %v, got %v", secondRetry, next)
	}

	_, ok = scheduler.popNextRetryRun()
	if !ok {
		t.Fatal("expected second retry to be consumable")
	}
	if got := scheduler.retryRunCount(); got != 0 {
		t.Fatalf("expected retry queue to be empty, got %d", got)
	}

	next, trigger = scheduler.nextRunWithTrigger(secondRetry.Add(1 * time.Minute))
	wantRegular := scheduler.calculateNextRegularRun(secondRetry.Add(1 * time.Minute))
	if trigger != scrapeTriggerRegular {
		t.Fatalf("expected regular trigger after retry consumption, got %s", trigger)
	}
	if !next.Equal(wantRegular) {
		t.Fatalf("expected next regular run %v, got %v", wantRegular, next)
	}
}
