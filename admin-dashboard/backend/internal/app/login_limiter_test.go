package app

import (
	"fmt"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
)

func newTestDistributedLoginLimiter(t *testing.T) *distributedLoginLimiter {
	t.Helper()

	mr := miniredis.RunT(t)
	limiter, err := newDistributedLoginLimiter(t.Context(), mr.Addr())
	require.NoError(t, err)
	t.Cleanup(limiter.Close)

	return limiter
}

func TestDistributedLoginLimiterBlocksByIPAndClearsOnSuccess(t *testing.T) {
	limiter := newTestDistributedLoginLimiter(t)
	ctx := t.Context()

	for range loginIPFailureLimit {
		_, err := limiter.RecordFailure(ctx, "203.0.113.10", "admin")
		require.NoError(t, err)
	}

	retry, err := limiter.Check(ctx, "203.0.113.10", "admin")
	require.NoError(t, err)
	require.Positive(t, retry)

	require.NoError(t, limiter.RecordSuccess(ctx, "203.0.113.10", "admin"))

	retry, err = limiter.Check(ctx, "203.0.113.10", "admin")
	require.NoError(t, err)
	require.Zero(t, retry, "successful authentication clears IP/account debt; global debt remains below threshold")
}

func TestDistributedLoginLimiterBlocksAccountAcrossIPs(t *testing.T) {
	limiter := newTestDistributedLoginLimiter(t)
	ctx := t.Context()

	for i := range loginAccountFailureLimit {
		_, err := limiter.RecordFailure(ctx, fmt.Sprintf("198.51.100.%d", i+1), "admin")
		require.NoError(t, err)
	}

	retry, err := limiter.Check(ctx, "192.0.2.99", "admin")
	require.NoError(t, err)
	require.Positive(t, retry)
}

func TestDistributedLoginLimiterBlocksGlobalAcrossAccountsAndIPs(t *testing.T) {
	limiter := newTestDistributedLoginLimiter(t)
	ctx := t.Context()

	for i := range loginGlobalFailureLimit {
		_, err := limiter.RecordFailure(ctx, fmt.Sprintf("198.18.%d.%d", (i/250)+1, (i%250)+1), fmt.Sprintf("account-%d", i))
		require.NoError(t, err)
	}

	retry, err := limiter.Check(ctx, "192.0.2.1", "otherwise-clean-account")
	require.NoError(t, err)
	require.Positive(t, retry)
}

func TestLoginLimiterKeysDoNotExposeRawDimensions(t *testing.T) {
	keys := loginLimiterKeys("203.0.113.42", "sensitive-admin-name")
	require.NotContains(t, keys[0], "203.0.113.42")
	require.NotContains(t, keys[1], "sensitive-admin-name")
	require.Equal(t, "login:admin:limit:global", keys[2])
}
