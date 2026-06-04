package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRateLimiterLockoutAfterMaxFailures(t *testing.T) {
	limiter := NewLoginRateLimiter()

	for i := range 5 {
		require.Equal(t, i+1, limiter.RecordFailure("203.0.113.1"))
	}

	allowed, retryAfter := limiter.IsAllowed("203.0.113.1")
	require.False(t, allowed)
	require.Greater(t, retryAfter, time.Duration(0))

	allowed, _ = limiter.IsAllowed("203.0.113.2")
	require.True(t, allowed)
}

func TestRateLimiterSuccessClearsFailures(t *testing.T) {
	limiter := NewLoginRateLimiter()

	for range 4 {
		limiter.RecordFailure("203.0.113.3")
	}
	limiter.RecordSuccess("203.0.113.3")

	allowed, _ := limiter.IsAllowed("203.0.113.3")
	require.True(t, allowed)
	require.Equal(t, 1, limiter.RecordFailure("203.0.113.3"))
}

func TestRateLimiterCleanupDropsStaleEntries(t *testing.T) {
	limiter := NewLoginRateLimiter()
	limiter.RecordFailure("203.0.113.4")
	for range 5 {
		limiter.RecordFailure("203.0.113.5")
	}

	limiter.cleanup(time.Now().Add(time.Hour))

	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	require.Empty(t, limiter.attempts)
}

func TestRateLimiterStartStop(t *testing.T) {
	limiter := NewLoginRateLimiter()
	limiter.Start()
	limiter.Stop()
	limiter.Stop()
}
