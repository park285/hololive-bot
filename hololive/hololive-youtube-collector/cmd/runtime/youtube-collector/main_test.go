package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestYouTubeCollectorLogFileNameUsesExplicitEnv(t *testing.T) {
	t.Setenv("YOUTUBE_COLLECTOR_LOG_FILE_NAME", "youtube-collector-central.log")

	if got := youtubeCollectorLogFileName(); got != "youtube-collector-central.log" {
		t.Fatalf("youtubeCollectorLogFileName() = %q, want youtube-collector-central.log", got)
	}
}

func TestRunWorkerProfileCheck(t *testing.T) {
	var stderr bytes.Buffer

	handled, code := runWorkerProfileCheck([]string{"--check-worker-profile"}, &stderr, func() error { return nil })

	if !handled || code != 0 || !strings.Contains(stderr.String(), "worker profile valid") {
		t.Fatalf("runWorkerProfileCheck() = (%t,%d,%q)", handled, code, stderr.String())
	}

	stderr.Reset()

	handled, code = runWorkerProfileCheck([]string{"--check-worker-profile"}, &stderr, func() error { return errors.New("invalid profile") })
	if !handled || code != 1 || !strings.Contains(stderr.String(), "invalid profile") {
		t.Fatalf("runWorkerProfileCheck() failure = (%t,%d,%q)", handled, code, stderr.String())
	}
}

func TestYouTubeCollectorLogFileNameRejectsPathSeparators(t *testing.T) {
	t.Setenv("YOUTUBE_COLLECTOR_LOG_FILE_NAME", "logs/youtube-collector.log")

	if got := youtubeCollectorLogFileName(); got != "youtube-collector.log" {
		t.Fatalf("youtubeCollectorLogFileName() = %q, want youtube-collector.log", got)
	}
}
