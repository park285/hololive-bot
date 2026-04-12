package main

import (
	"testing"
	"time"
)

func TestValidateCommunityShortsSendCountCLIArgs(t *testing.T) {
	t.Parallel()

	t.Run("accepts recent window query", func(t *testing.T) {
		err := validateCommunityShortsSendCountCLIArgs(24*time.Hour, true, "", "")
		if err != nil {
			t.Fatalf("validateCommunityShortsSendCountCLIArgs() error = %v", err)
		}
	})

	t.Run("accepts observation query without explicit window", func(t *testing.T) {
		err := validateCommunityShortsSendCountCLIArgs(24*time.Hour, false, "youtube-scraper", "2026-04-10T00:00:00Z")
		if err != nil {
			t.Fatalf("validateCommunityShortsSendCountCLIArgs() error = %v", err)
		}
	})

	t.Run("rejects mixed recent and observation query", func(t *testing.T) {
		err := validateCommunityShortsSendCountCLIArgs(24*time.Hour, true, "youtube-scraper", "2026-04-10T00:00:00Z")
		if err == nil || err.Error() != "window and observation query flags are mutually exclusive" {
			t.Fatalf("validateCommunityShortsSendCountCLIArgs() error = %v", err)
		}
	})

	t.Run("rejects partial observation query", func(t *testing.T) {
		err := validateCommunityShortsSendCountCLIArgs(24*time.Hour, false, "youtube-scraper", "")
		if err == nil || err.Error() != "observation-runtime and observation-cutover must be provided together" {
			t.Fatalf("validateCommunityShortsSendCountCLIArgs() error = %v", err)
		}
	})
}
