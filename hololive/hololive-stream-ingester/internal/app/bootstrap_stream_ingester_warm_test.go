package app

import (
	"context"
	"errors"
	"testing"
)

func TestWarmSubscriberCacheOnYouTubeStartupRunsWhenYouTubeIsEnabled(t *testing.T) {
	t.Parallel()

	calls := 0
	err := warmSubscriberCacheOnYouTubeStartup(
		context.Background(),
		"stream-ingester",
		true,
		func(context.Context) error {
			calls++
			return nil
		},
	)
	if err != nil {
		t.Fatalf("warmSubscriberCacheOnYouTubeStartup() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("warmSubscriberCacheOnYouTubeStartup() calls = %d, want 1", calls)
	}
}

func TestWarmSubscriberCacheOnYouTubeStartupSkipsWhenYouTubeIsDisabled(t *testing.T) {
	t.Parallel()

	calls := 0
	err := warmSubscriberCacheOnYouTubeStartup(
		context.Background(),
		"stream-ingester",
		false,
		func(context.Context) error {
			calls++
			return nil
		},
	)
	if err != nil {
		t.Fatalf("warmSubscriberCacheOnYouTubeStartup() error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("warmSubscriberCacheOnYouTubeStartup() calls = %d, want 0", calls)
	}
}

func TestWarmSubscriberCacheOnYouTubeStartupReturnsWarmError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("warm failed")
	err := warmSubscriberCacheOnYouTubeStartup(
		context.Background(),
		"youtube-scraper",
		true,
		func(context.Context) error {
			return wantErr
		},
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("warmSubscriberCacheOnYouTubeStartup() error = %v, want %v", err, wantErr)
	}
}
