package main

import "testing"

func TestValidateCommunityShortsSendStateCLIArgs(t *testing.T) {
	t.Parallel()

	t.Run("accepts observation query", func(t *testing.T) {
		err := validateCommunityShortsSendStateCLIArgs("youtube-scraper", "2026-04-10T00:00:00Z")
		if err != nil {
			t.Fatalf("validateCommunityShortsSendStateCLIArgs() error = %v", err)
		}
	})

	t.Run("rejects empty query", func(t *testing.T) {
		err := validateCommunityShortsSendStateCLIArgs("", "")
		if err == nil || err.Error() != "observation-runtime and observation-cutover are required" {
			t.Fatalf("validateCommunityShortsSendStateCLIArgs() error = %v", err)
		}
	})

	t.Run("rejects partial query", func(t *testing.T) {
		err := validateCommunityShortsSendStateCLIArgs("youtube-scraper", "")
		if err == nil || err.Error() != "observation-runtime and observation-cutover must be provided together" {
			t.Fatalf("validateCommunityShortsSendStateCLIArgs() error = %v", err)
		}
	})
}
