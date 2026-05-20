package observationquery

import (
	"testing"
	"time"
)

func TestParseRequired(t *testing.T) {
	t.Parallel()

	t.Run("accepts complete observation query", func(t *testing.T) {
		query, err := ParseRequired(" youtube-producer ", "2026-04-10T00:00:00Z")
		if err != nil {
			t.Fatalf("ParseRequired() error = %v", err)
		}
		if query.Runtime != "youtube-producer" {
			t.Fatalf("ParseRequired() runtime = %q", query.Runtime)
		}
		expectedCutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
		if query.CutoverAt != expectedCutoverAt {
			t.Fatalf("ParseRequired() cutover = %s, want %s", query.CutoverAt, expectedCutoverAt)
		}
	})

	t.Run("rejects missing observation query", func(t *testing.T) {
		_, err := ParseRequired("", "")
		if err == nil || err.Error() != "observation-runtime and observation-cutover are required" {
			t.Fatalf("ParseRequired() error = %v", err)
		}
	})

	t.Run("rejects partial observation query", func(t *testing.T) {
		_, err := ParseRequired("youtube-producer", "")
		if err == nil || err.Error() != "observation-runtime and observation-cutover must be provided together" {
			t.Fatalf("ParseRequired() error = %v", err)
		}
	})

	t.Run("rejects invalid cutover", func(t *testing.T) {
		_, err := ParseRequired("youtube-producer", "not-a-time")
		if err == nil || err.Error() == "" {
			t.Fatalf("ParseRequired() error = %v", err)
		}
	})
}

func TestParseOptional(t *testing.T) {
	t.Parallel()

	t.Run("accepts empty observation query", func(t *testing.T) {
		query, ok, err := ParseOptional("", "")
		if err != nil {
			t.Fatalf("ParseOptional() error = %v", err)
		}
		if ok {
			t.Fatalf("ParseOptional() ok = true, query = %+v", query)
		}
	})

	t.Run("accepts complete observation query", func(t *testing.T) {
		query, ok, err := ParseOptional("youtube-producer", "2026-04-10T00:00:00Z")
		if err != nil {
			t.Fatalf("ParseOptional() error = %v", err)
		}
		if !ok {
			t.Fatal("ParseOptional() ok = false")
		}
		expectedCutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
		if query.Runtime != "youtube-producer" || query.CutoverAt != expectedCutoverAt {
			t.Fatalf("ParseOptional() query = %+v", query)
		}
	})

	t.Run("rejects partial observation query", func(t *testing.T) {
		_, _, err := ParseOptional("youtube-producer", "")
		if err == nil || err.Error() != "observation-runtime and observation-cutover must be provided together" {
			t.Fatalf("ParseOptional() error = %v", err)
		}
	})
}
