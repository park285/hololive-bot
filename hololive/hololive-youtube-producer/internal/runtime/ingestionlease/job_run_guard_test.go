package ingestionlease

import (
	"context"
	"strings"
	"testing"
	"time"

	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
	"github.com/stretchr/testify/require"
)

func TestJobRunGuardClaimBlocksSameJobAndAllowsDifferentChannels(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache := sharedtestutil.NewTestCacheService(t, ctx)
	guard := NewJobRunGuard(cache, JobRunGuardConfig{
		Namespace:  "test",
		InstanceID: "ap-a",
	})

	identity := JobIdentity{PollerName: "videos", ChannelID: "UC_A", Interval: time.Minute}
	first, claim, err := guard.TryLease(ctx, identity, time.Minute, identity.Interval)
	require.NoError(t, err)
	require.Equal(t, JobClaimAcquired, first.Result)
	require.NotNil(t, claim)

	second, peerClaim, err := guard.TryLease(ctx, identity, time.Minute, identity.Interval)
	require.NoError(t, err)
	require.Equal(t, JobClaimPeerOwned, second.Result)
	require.Nil(t, peerClaim)
	require.Greater(t, second.RetryAfter, time.Duration(0))

	other, otherClaim, err := guard.TryLease(ctx, JobIdentity{PollerName: "videos", ChannelID: "UC_B", Interval: time.Minute}, time.Minute, time.Minute)
	require.NoError(t, err)
	require.Equal(t, JobClaimAcquired, other.Result)
	require.NotNil(t, otherClaim)
}

func TestJobRunGuardMarkCompletedCreatesCooldown(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache := sharedtestutil.NewTestCacheService(t, ctx)
	guard := NewJobRunGuard(cache, JobRunGuardConfig{
		Namespace:  "test",
		InstanceID: "ap-a",
	})
	identity := JobIdentity{PollerName: "community", ChannelID: "UC_A", Interval: 2 * time.Minute}

	status, claim, err := guard.TryLease(ctx, identity, time.Minute, identity.Interval)
	require.NoError(t, err)
	require.Equal(t, JobClaimAcquired, status.Result)

	completed, err := claim.MarkCompleted(ctx, identity.Interval)
	require.NoError(t, err)
	require.True(t, completed)

	next, nextClaim, err := guard.TryLease(ctx, identity, time.Minute, identity.Interval)
	require.NoError(t, err)
	require.Equal(t, JobClaimAlreadyCompleted, next.Result)
	require.Nil(t, nextClaim)
	require.Greater(t, next.RetryAfter, time.Duration(0))
}

func TestJobRunGuardDeferCreatesCooldown(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache := sharedtestutil.NewTestCacheService(t, ctx)
	guard := NewJobRunGuard(cache, JobRunGuardConfig{
		Namespace:  "test",
		InstanceID: "ap-a",
	})
	identity := JobIdentity{PollerName: "community", ChannelID: "UC_DEFER", Interval: time.Minute}

	status, claim, err := guard.TryLease(ctx, identity, time.Minute, identity.Interval)
	require.NoError(t, err)
	require.Equal(t, JobClaimAcquired, status.Result)

	deferred, err := claim.Defer(ctx, 5*time.Second)
	require.NoError(t, err)
	require.True(t, deferred)

	next, nextClaim, err := guard.TryLease(ctx, identity, time.Minute, identity.Interval)
	require.NoError(t, err)
	require.Equal(t, JobClaimAlreadyCompleted, next.Result)
	require.Nil(t, nextClaim)
	require.Greater(t, next.RetryAfter, time.Duration(0))
	require.LessOrEqual(t, next.RetryAfter, 5*time.Second)
}

func TestJobRunGuardWinnerCompleteMakesPeerAlreadyCompleted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache := sharedtestutil.NewTestCacheService(t, ctx)
	winner := NewJobRunGuard(cache, JobRunGuardConfig{Namespace: "test", InstanceID: "ap-a"})
	peer := NewJobRunGuard(cache, JobRunGuardConfig{Namespace: "test", InstanceID: "ap-b"})
	identity := JobIdentity{PollerName: "videos", ChannelID: "UC_RACE", Interval: 250 * time.Millisecond}

	status, claim, err := winner.TryLease(ctx, identity, time.Second, identity.Interval)
	require.NoError(t, err)
	require.Equal(t, JobClaimAcquired, status.Result)

	status, peerClaim, err := peer.TryLease(ctx, identity, time.Second, identity.Interval)
	require.NoError(t, err)
	require.Equal(t, JobClaimPeerOwned, status.Result)
	require.Nil(t, peerClaim)

	completed, err := claim.MarkCompleted(ctx, identity.Interval)
	require.NoError(t, err)
	require.True(t, completed)

	status, peerClaim, err = peer.TryLease(ctx, identity, time.Second, identity.Interval)
	require.NoError(t, err)
	require.Equal(t, JobClaimAlreadyCompleted, status.Result)
	require.Nil(t, peerClaim)
}

func TestJobRunGuardExpiredLeaseCanFailOverToPeer(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache, mini := sharedtestutil.NewTestCacheServiceWithMini(t, ctx)
	first := NewJobRunGuard(cache, JobRunGuardConfig{Namespace: "test", InstanceID: "ap-a"})
	second := NewJobRunGuard(cache, JobRunGuardConfig{Namespace: "test", InstanceID: "ap-b"})
	identity := JobIdentity{PollerName: "live", ChannelID: "UC_FAILOVER", Interval: time.Second}

	status, claim, err := first.TryLease(ctx, identity, 20*time.Millisecond, identity.Interval)
	require.NoError(t, err)
	require.Equal(t, JobClaimAcquired, status.Result)
	require.NotNil(t, claim)

	mini.FastForward(21 * time.Millisecond)
	status, claim, err = second.TryLease(ctx, identity, time.Second, identity.Interval)
	require.NoError(t, err)
	require.Equal(t, JobClaimAcquired, status.Result)
	require.NotNil(t, claim)
}

func TestJobRunGuardMarkCompletedUsesOwnerCAS(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache := sharedtestutil.NewTestCacheService(t, ctx)
	guard := NewJobRunGuard(cache, JobRunGuardConfig{
		Namespace:  "test",
		InstanceID: "ap-a",
	})
	identity := JobIdentity{PollerName: "shorts", ChannelID: "UC_CAS", Interval: time.Minute}

	_, claim, err := guard.TryLease(ctx, identity, time.Minute, identity.Interval)
	require.NoError(t, err)
	require.NoError(t, cache.GetClient().Do(ctx, cache.B().Set().Key(claim.LeaseKey()).Value("peer-owner").Build()).Error())

	completed, err := claim.MarkCompleted(ctx, identity.Interval)
	require.NoError(t, err)
	require.False(t, completed)

	_, hit, err := cache.GetString(ctx, claim.CooldownKey())
	require.NoError(t, err)
	require.False(t, hit)
}

func TestJobRunGuardDeferUsesOwnerCAS(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache := sharedtestutil.NewTestCacheService(t, ctx)
	guard := NewJobRunGuard(cache, JobRunGuardConfig{
		Namespace:  "test",
		InstanceID: "ap-a",
	})
	identity := JobIdentity{PollerName: "shorts", ChannelID: "UC_DEFER_CAS", Interval: time.Minute}

	_, claim, err := guard.TryLease(ctx, identity, time.Minute, identity.Interval)
	require.NoError(t, err)
	require.NoError(t, cache.GetClient().Do(ctx, cache.B().Set().Key(claim.LeaseKey()).Value("peer-owner").Build()).Error())

	deferred, err := claim.Defer(ctx, 5*time.Second)
	require.NoError(t, err)
	require.False(t, deferred)

	_, hit, err := cache.GetString(ctx, claim.CooldownKey())
	require.NoError(t, err)
	require.False(t, hit)
}

func TestJobRunGuardDeferRejectsNonPositiveTTL(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache := sharedtestutil.NewTestCacheService(t, ctx)
	guard := NewJobRunGuard(cache, JobRunGuardConfig{
		Namespace:  "test",
		InstanceID: "ap-a",
	})
	identity := JobIdentity{PollerName: "shorts", ChannelID: "UC_DEFER_TTL", Interval: time.Minute}

	_, claim, err := guard.TryLease(ctx, identity, time.Minute, identity.Interval)
	require.NoError(t, err)

	deferred, err := claim.Defer(ctx, 0)
	require.Error(t, err)
	require.False(t, deferred)
}

func TestJobRunGuardRenewAndReleaseUseOwnerCAS(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache := sharedtestutil.NewTestCacheService(t, ctx)
	guard := NewJobRunGuard(cache, JobRunGuardConfig{
		Namespace:  "test",
		InstanceID: "ap-a",
	})
	identity := JobIdentity{PollerName: "shorts", ChannelID: "UC_A", Interval: time.Minute}

	_, claim, err := guard.TryLease(ctx, identity, time.Minute, identity.Interval)
	require.NoError(t, err)

	renewed, err := claim.Renew(ctx, time.Minute)
	require.NoError(t, err)
	require.True(t, renewed)

	require.NoError(t, cache.GetClient().Do(ctx, cache.B().Set().Key(claim.LeaseKey()).Value("peer-owner").Build()).Error())

	released, err := claim.Release(ctx)
	require.NoError(t, err)
	require.False(t, released)

	value, hit, err := cache.GetString(ctx, claim.LeaseKey())
	require.NoError(t, err)
	require.True(t, hit)
	require.Equal(t, "peer-owner", value)
}

func TestBuildJobLeaseKeysRejectsEmptyAndUsesHashTaggedKeys(t *testing.T) {
	t.Parallel()

	_, err := BuildJobLeaseKeys("prod", JobIdentity{PollerName: "videos", ChannelID: "   ", Interval: time.Minute})
	require.Error(t, err)

	keys, err := BuildJobLeaseKeys("prod", JobIdentity{PollerName: " videos ", ChannelID: " UC_A ", Interval: time.Minute})
	require.NoError(t, err)
	require.Contains(t, keys.LeaseKey, "hololive:prod:youtube-producer:{job:")
	require.True(t, strings.HasSuffix(keys.LeaseKey, "}:lease"))
	require.True(t, strings.HasSuffix(keys.CooldownKey, "}:cooldown"))
	require.Equal(t, strings.TrimSuffix(keys.LeaseKey, ":lease"), strings.TrimSuffix(keys.CooldownKey, ":cooldown"))
}
