package scraping

import "testing"

func TestClientRateLimiterConfigured(t *testing.T) {
	t.Parallel()

	if !NewClient().RateLimiterConfigured() {
		t.Fatal("default client must have a rate limiter")
	}
	if NewClient(WithRateLimiter(nil)).RateLimiterConfigured() {
		t.Fatal("client explicitly configured without limiter reported one")
	}
}
