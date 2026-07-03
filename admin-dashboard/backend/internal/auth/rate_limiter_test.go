package auth

import (
	"testing"
	"time"

	"github.com/park285/shared-go/pkg/httputil"
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

func TestRateLimiterWindowExpiryStartsFreshAttempt(t *testing.T) {
	now := time.Now()
	limiter := httputil.NewLoginFailureRateLimiter(httputil.LoginFailureRateLimiterOptions{
		Now: func() time.Time { return now },
	})
	ip := "203.0.113.4"

	require.Equal(t, 1, limiter.RecordFailure(ip))
	now = now.Add(6 * time.Minute)
	require.Equal(t, 1, limiter.RecordFailure(ip))
}

func TestRateLimiterKeepsActiveLockoutAfterWindowExpires(t *testing.T) {
	now := time.Now()
	limiter := httputil.NewLoginFailureRateLimiter(httputil.LoginFailureRateLimiterOptions{
		Now: func() time.Time { return now },
	})
	ip := "203.0.113.6"

	for range 5 {
		limiter.RecordFailure(ip)
	}
	now = now.Add(10 * time.Minute)

	allowed, retryAfter := limiter.IsAllowed(ip)
	require.False(t, allowed)
	require.Greater(t, retryAfter, time.Duration(0))
}

func TestRateLimiterExpiredLockoutAllowsLogin(t *testing.T) {
	now := time.Now()
	limiter := httputil.NewLoginFailureRateLimiter(httputil.LoginFailureRateLimiterOptions{
		Now: func() time.Time { return now },
	})
	ip := "203.0.113.7"

	for range 5 {
		limiter.RecordFailure(ip)
	}
	now = now.Add(16 * time.Minute)

	allowed, retryAfter := limiter.IsAllowed(ip)
	require.True(t, allowed)
	require.Zero(t, retryAfter)
	require.Equal(t, 1, limiter.RecordFailure(ip))
}

func TestRateLimiterStartStop(t *testing.T) {
	limiter := NewLoginRateLimiter()
	limiter.Start()
	limiter.Stop()
	limiter.Stop()
}

func TestRateLimiterStopBeforeStart(t *testing.T) {
	limiter := NewLoginRateLimiter()
	limiter.Stop()
	limiter.Stop()
}
