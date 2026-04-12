package main

import "testing"

func TestValidateShortsAlarmSentHistoryCLIArgs(t *testing.T) {
	t.Parallel()

	t.Run("accepts complete observation key", func(t *testing.T) {
		err := validateShortsAlarmSentHistoryCLIArgs("youtube-scraper", "2026-04-10T00:00:00Z")
		if err != nil {
			t.Fatalf("validateShortsAlarmSentHistoryCLIArgs() error = %v", err)
		}
	})

	t.Run("rejects missing observation key", func(t *testing.T) {
		err := validateShortsAlarmSentHistoryCLIArgs("", "")
		if err == nil || err.Error() != "observation-runtime and observation-cutover are required" {
			t.Fatalf("validateShortsAlarmSentHistoryCLIArgs() error = %v", err)
		}
	})

	t.Run("rejects partial observation key", func(t *testing.T) {
		err := validateShortsAlarmSentHistoryCLIArgs("youtube-scraper", "")
		if err == nil || err.Error() != "observation-runtime and observation-cutover must be provided together" {
			t.Fatalf("validateShortsAlarmSentHistoryCLIArgs() error = %v", err)
		}
	})
}
