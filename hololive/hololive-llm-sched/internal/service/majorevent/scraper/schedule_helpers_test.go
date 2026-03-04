package scraper

import (
	"testing"
	"time"
)

func TestCalculateNextRunAtHour(t *testing.T) {
	now := time.Date(2026, time.March, 4, 18, 0, 0, 0, time.UTC) // 03:00 KST

	next := calculateNextRunAtHour(now, 4)
	want := time.Date(2026, time.March, 4, 19, 0, 0, 0, time.UTC) // 04:00 KST

	if !next.Equal(want) {
		t.Fatalf("calculateNextRunAtHour() = %s, want %s", next, want)
	}
}

func TestBuildRetryRuns(t *testing.T) {
	baseRun := time.Date(2026, time.March, 4, 19, 0, 0, 0, time.UTC)  // KST 04:00
	failedAt := time.Date(2026, time.March, 4, 19, 5, 0, 0, time.UTC) // KST 04:05
	crossDay := 21 * time.Hour                                        // KST +21h -> next day 01:00
	retries := buildRetryRuns(baseRun, failedAt, []time.Duration{30 * time.Minute, 2 * time.Hour, crossDay})

	if len(retries) != 2 {
		t.Fatalf("buildRetryRuns() len = %d, want 2", len(retries))
	}
}
