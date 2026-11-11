package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestYouTubeCollectorLogFileNameUsesExplicitEnv(t *testing.T) {
	t.Setenv("YOUTUBE_COLLECTOR_LOG_FILE_NAME", "youtube-collector-central.log")

	if got, err := youtubeCollectorLogFileName(); err != nil || got != "youtube-collector-central.log" {
		t.Fatalf("youtubeCollectorLogFileName() = (%q, %v), want youtube-collector-central.log", got, err)
	}
}

func TestYouTubeCollectorLogFileNameDefaultsWhenUnset(t *testing.T) {
	t.Setenv("YOUTUBE_COLLECTOR_LOG_FILE_NAME", "")

	if got, err := youtubeCollectorLogFileName(); err != nil || got != "youtube-collector.log" {
		t.Fatalf("youtubeCollectorLogFileName() = (%q, %v), want youtube-collector.log", got, err)
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
	for _, name := range []string{"logs/youtube-collector.log", `logs\youtube-collector.log`} {
		t.Setenv("YOUTUBE_COLLECTOR_LOG_FILE_NAME", name)

		if got, err := youtubeCollectorLogFileName(); err == nil {
			t.Fatalf("youtubeCollectorLogFileName(%q) = %q, want startup error", name, got)
		}
	}
}
