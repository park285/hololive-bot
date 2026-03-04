package providers

import (
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/constants"
)

func TestProvideYouTubeScraperRateLimiter_DisabledDistributed_AllowsNilCache(t *testing.T) {
	original := constants.YouTubeScraperDistributedRateLimitConfig
	t.Cleanup(func() {
		constants.YouTubeScraperDistributedRateLimitConfig = original
	})

	constants.YouTubeScraperDistributedRateLimitConfig.Enabled = false

	limiter, err := ProvideYouTubeScraperRateLimiter(nil, nil)
	if err != nil {
		t.Fatalf("ProvideYouTubeScraperRateLimiter() error = %v, want nil", err)
	}
	if limiter == nil {
		t.Fatal("ProvideYouTubeScraperRateLimiter() limiter is nil")
	}
}

func TestProvideYouTubeScraperRateLimiter_EnabledDistributed_RequiresCache(t *testing.T) {
	original := constants.YouTubeScraperDistributedRateLimitConfig
	t.Cleanup(func() {
		constants.YouTubeScraperDistributedRateLimitConfig = original
	})

	constants.YouTubeScraperDistributedRateLimitConfig.Enabled = true

	limiter, err := ProvideYouTubeScraperRateLimiter(nil, nil)
	if err == nil {
		t.Fatal("ProvideYouTubeScraperRateLimiter() expected error, got nil")
	}
	if limiter != nil {
		t.Fatal("ProvideYouTubeScraperRateLimiter() limiter must be nil on error")
	}
	if !strings.Contains(err.Error(), "initialize scraper distributed rate limiter") {
		t.Fatalf("ProvideYouTubeScraperRateLimiter() error = %q, want contains %q", err.Error(), "initialize scraper distributed rate limiter")
	}
}
