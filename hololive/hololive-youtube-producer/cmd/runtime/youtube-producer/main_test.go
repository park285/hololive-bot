package main

import "testing"

func TestYouTubeProducerLogFileNameUsesExplicitEnv(t *testing.T) {
	t.Setenv("YOUTUBE_PRODUCER_LOG_FILE_NAME", "youtube-producer-b.log")

	if got := youtubeProducerLogFileName(); got != "youtube-producer-b.log" {
		t.Fatalf("youtubeProducerLogFileName() = %q, want %q", got, "youtube-producer-b.log")
	}
}

func TestYouTubeProducerLogFileNameDefaultsToLegacyName(t *testing.T) {
	t.Setenv("YOUTUBE_PRODUCER_LOG_FILE_NAME", "")

	if got := youtubeProducerLogFileName(); got != "youtube-producer.log" {
		t.Fatalf("youtubeProducerLogFileName() = %q, want %q", got, "youtube-producer.log")
	}
}
