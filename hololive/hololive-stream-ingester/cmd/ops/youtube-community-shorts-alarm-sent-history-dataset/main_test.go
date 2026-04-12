package main

import "testing"

func TestValidateCommunityShortsAlarmSentHistoryDatasetCLIArgs(t *testing.T) {
	t.Parallel()

	t.Run("accepts complete observation key", func(t *testing.T) {
		err := validateCommunityShortsAlarmSentHistoryDatasetCLIArgs("youtube-scraper", "2026-04-10T00:00:00Z")
		if err != nil {
			t.Fatalf("validateCommunityShortsAlarmSentHistoryDatasetCLIArgs() error = %v", err)
		}
	})

	t.Run("rejects missing observation key", func(t *testing.T) {
		err := validateCommunityShortsAlarmSentHistoryDatasetCLIArgs("", "")
		if err == nil || err.Error() != "observation-runtime and observation-cutover are required" {
			t.Fatalf("validateCommunityShortsAlarmSentHistoryDatasetCLIArgs() error = %v", err)
		}
	})

	t.Run("rejects partial observation key", func(t *testing.T) {
		err := validateCommunityShortsAlarmSentHistoryDatasetCLIArgs("youtube-scraper", "")
		if err == nil || err.Error() != "observation-runtime and observation-cutover must be provided together" {
			t.Fatalf("validateCommunityShortsAlarmSentHistoryDatasetCLIArgs() error = %v", err)
		}
	})
}
