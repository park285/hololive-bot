package polling

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/cache"

	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
	"github.com/stretchr/testify/require"
)

func TestGlobalBudgetLimiterAllowsUnderCapAndRecordsInflight(t *testing.T) {
	ctx := context.Background()
	cacheClient := sharedtestutil.NewTestCacheService(t, ctx)
	limiter := newTestGlobalBudgetLimiter(t, cacheClient, GlobalBudgetLimiterConfig{
		SourceMaxInflight: map[polling.BudgetSource]int{polling.BudgetSourceYouTubeScraper: 2},
		ClassMaxInflight:  map[polling.BudgetBurstClass]int{polling.BudgetBurstPrimary: 2},
	})

	reservation, decision, err := limiter.TryReserve(ctx, testBudgetJob("under-cap"), testBudgetProfile(polling.BudgetSourceYouTubeScraper, polling.BudgetBurstPrimary), time.Minute)

	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.NotNil(t, reservation)
	require.Equal(t, 1, testInflightValue(t, ctx, cacheClient, testClassInflightKey(polling.BudgetSourceYouTubeScraper, polling.BudgetBurstPrimary)))
	require.Equal(t, 1, testInflightValue(t, ctx, cacheClient, testGlobalInflightKey(polling.BudgetSourceYouTubeScraper)))
}

func TestGlobalBudgetLimiterDeniesWhenSourceMaxInflightReached(t *testing.T) {
	ctx := context.Background()
	cacheClient := sharedtestutil.NewTestCacheService(t, ctx)
	limiter := newTestGlobalBudgetLimiter(t, cacheClient, GlobalBudgetLimiterConfig{
		SourceMaxInflight: map[polling.BudgetSource]int{polling.BudgetSourceYouTubeScraper: 1},
		ClassMaxInflight:  map[polling.BudgetBurstClass]int{polling.BudgetBurstPrimary: 5},
	})

	first, decision, err := limiter.TryReserve(ctx, testBudgetJob("source-cap-a"), testBudgetProfile(polling.BudgetSourceYouTubeScraper, polling.BudgetBurstPrimary), time.Minute)
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.NotNil(t, first)

	second, decision, err := limiter.TryReserve(ctx, testBudgetJob("source-cap-b"), testBudgetProfile(polling.BudgetSourceYouTubeScraper, polling.BudgetBurstPrimary), time.Minute)

	require.NoError(t, err)
	require.Nil(t, second)
	require.False(t, decision.Allowed)
	require.Equal(t, "budget_exhausted", decision.Reason)
	require.Equal(t, string(polling.BudgetSourceYouTubeScraper), decision.AffectedSource)
	require.Greater(t, decision.RetryAfter, time.Duration(0))
	require.Equal(t, 1, testInflightValue(t, ctx, cacheClient, testGlobalInflightKey(polling.BudgetSourceYouTubeScraper)))
}

func TestGlobalBudgetLimiterIsolatesBurstClassesWithinSource(t *testing.T) {
	ctx := context.Background()
	cacheClient := sharedtestutil.NewTestCacheService(t, ctx)
	limiter := newTestGlobalBudgetLimiter(t, cacheClient, GlobalBudgetLimiterConfig{
		SourceMaxInflight: map[polling.BudgetSource]int{polling.BudgetSourceYouTubeScraper: 10},
		ClassMaxInflight: map[polling.BudgetBurstClass]int{
			polling.BudgetBurstBackfill: 1,
			polling.BudgetBurstPrimary:  2,
		},
	})

	backfill, decision, err := limiter.TryReserve(ctx, testBudgetJob("backfill-a"), testBudgetProfile(polling.BudgetSourceYouTubeScraper, polling.BudgetBurstBackfill), time.Minute)
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.NotNil(t, backfill)

	denied, decision, err := limiter.TryReserve(ctx, testBudgetJob("backfill-b"), testBudgetProfile(polling.BudgetSourceYouTubeScraper, polling.BudgetBurstBackfill), time.Minute)
	require.NoError(t, err)
	require.Nil(t, denied)
	require.False(t, decision.Allowed)
	require.Equal(t, "budget_exhausted", decision.Reason)

	primary, decision, err := limiter.TryReserve(ctx, testBudgetJob("primary-a"), testBudgetProfile(polling.BudgetSourceYouTubeScraper, polling.BudgetBurstPrimary), time.Minute)
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.NotNil(t, primary)
	require.Equal(t, 1, testInflightValue(t, ctx, cacheClient, testClassInflightKey(polling.BudgetSourceYouTubeScraper, polling.BudgetBurstBackfill)))
	require.Equal(t, 1, testInflightValue(t, ctx, cacheClient, testClassInflightKey(polling.BudgetSourceYouTubeScraper, polling.BudgetBurstPrimary)))
}

func TestGlobalBudgetLimiterRollsBackEarlierSourceWhenLaterSourceDenied(t *testing.T) {
	ctx := context.Background()
	cacheClient := sharedtestutil.NewTestCacheService(t, ctx)
	limiter := newTestGlobalBudgetLimiter(t, cacheClient, GlobalBudgetLimiterConfig{
		SourceMaxInflight: map[polling.BudgetSource]int{
			polling.BudgetSourceHolodexLive:    2,
			polling.BudgetSourceYouTubeScraper: 1,
		},
		ClassMaxInflight: map[polling.BudgetBurstClass]int{polling.BudgetBurstPrimary: 10},
	})

	held, decision, err := limiter.TryReserve(ctx, testBudgetJob("held-youtube"), testBudgetProfile(polling.BudgetSourceYouTubeScraper, polling.BudgetBurstPrimary), time.Minute)
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.NotNil(t, held)

	profile := polling.BudgetProfile{
		SourceUnits: map[polling.BudgetSource]float64{
			polling.BudgetSourceHolodexLive:    1,
			polling.BudgetSourceYouTubeScraper: 1,
		},
		BurstClass: polling.BudgetBurstPrimary,
		Priority:   polling.BudgetPriorityNormal,
	}
	rolledBack, decision, err := limiter.TryReserve(ctx, testBudgetJob("multi-source"), profile, time.Minute)

	require.NoError(t, err)
	require.Nil(t, rolledBack)
	require.False(t, decision.Allowed)
	require.Equal(t, "budget_exhausted", decision.Reason)
	require.Equal(t, 0, testInflightValue(t, ctx, cacheClient, testGlobalInflightKey(polling.BudgetSourceHolodexLive)))
	require.Equal(t, 0, testInflightValue(t, ctx, cacheClient, testClassInflightKey(polling.BudgetSourceHolodexLive, polling.BudgetBurstPrimary)))
	require.Equal(t, 1, testInflightValue(t, ctx, cacheClient, testGlobalInflightKey(polling.BudgetSourceYouTubeScraper)))
}

func TestGlobalBudgetLimiterReservationTerminalOperationsAreIdempotent(t *testing.T) {
	ctx := context.Background()

	t.Run("CommitThenRelease", func(t *testing.T) {
		cacheClient := sharedtestutil.NewTestCacheService(t, ctx)
		limiter := newTestGlobalBudgetLimiter(t, cacheClient, GlobalBudgetLimiterConfig{
			SourceMaxInflight: map[polling.BudgetSource]int{polling.BudgetSourceYouTubeScraper: 5},
		})
		reservation := testAllowedReservation(t, ctx, limiter, "commit-release")

		require.NoError(t, reservation.Commit(ctx))
		require.NoError(t, reservation.Release(ctx))
		require.Equal(t, 0, testInflightValue(t, ctx, cacheClient, testGlobalInflightKey(polling.BudgetSourceYouTubeScraper)))
		require.Equal(t, 0, testInflightValue(t, ctx, cacheClient, testClassInflightKey(polling.BudgetSourceYouTubeScraper, polling.BudgetBurstPrimary)))
	})

	t.Run("ReleaseThenCommit", func(t *testing.T) {
		cacheClient := sharedtestutil.NewTestCacheService(t, ctx)
		limiter := newTestGlobalBudgetLimiter(t, cacheClient, GlobalBudgetLimiterConfig{
			SourceMaxInflight: map[polling.BudgetSource]int{polling.BudgetSourceYouTubeScraper: 5},
		})
		reservation := testAllowedReservation(t, ctx, limiter, "release-commit")

		require.NoError(t, reservation.Release(ctx))
		require.NoError(t, reservation.Commit(ctx))
		require.Equal(t, 0, testInflightValue(t, ctx, cacheClient, testGlobalInflightKey(polling.BudgetSourceYouTubeScraper)))
		require.Equal(t, 0, testInflightValue(t, ctx, cacheClient, testClassInflightKey(polling.BudgetSourceYouTubeScraper, polling.BudgetBurstPrimary)))
	})

	t.Run("DoubleCommit", func(t *testing.T) {
		cacheClient := sharedtestutil.NewTestCacheService(t, ctx)
		limiter := newTestGlobalBudgetLimiter(t, cacheClient, GlobalBudgetLimiterConfig{
			SourceMaxInflight: map[polling.BudgetSource]int{polling.BudgetSourceYouTubeScraper: 5},
		})
		reservation := testAllowedReservation(t, ctx, limiter, "double-commit")

		require.NoError(t, reservation.Commit(ctx))
		require.NoError(t, reservation.Commit(ctx))
		require.Equal(t, 0, testInflightValue(t, ctx, cacheClient, testGlobalInflightKey(polling.BudgetSourceYouTubeScraper)))
		require.Equal(t, 0, testInflightValue(t, ctx, cacheClient, testClassInflightKey(polling.BudgetSourceYouTubeScraper, polling.BudgetBurstPrimary)))
	})
}

func TestGlobalBudgetLimiterCleansExpiredReservationOnNextTryReserve(t *testing.T) {
	ctx := context.Background()
	cacheClient := sharedtestutil.NewTestCacheService(t, ctx)
	limiter := newTestGlobalBudgetLimiter(t, cacheClient, GlobalBudgetLimiterConfig{
		SourceMaxInflight: map[polling.BudgetSource]int{polling.BudgetSourceYouTubeScraper: 1},
		ClassMaxInflight:  map[polling.BudgetBurstClass]int{polling.BudgetBurstPrimary: 1},
	})

	first, decision, err := limiter.TryReserve(ctx, testBudgetJob("expires-a"), testBudgetProfile(polling.BudgetSourceYouTubeScraper, polling.BudgetBurstPrimary), time.Millisecond)
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.NotNil(t, first)
	time.Sleep(5 * time.Millisecond)

	second, decision, err := limiter.TryReserve(ctx, testBudgetJob("expires-b"), testBudgetProfile(polling.BudgetSourceYouTubeScraper, polling.BudgetBurstPrimary), time.Minute)
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.NotNil(t, second)
	require.Equal(t, 1, testInflightValue(t, ctx, cacheClient, testGlobalInflightKey(polling.BudgetSourceYouTubeScraper)))
	require.Equal(t, 1, testInflightValue(t, ctx, cacheClient, testClassInflightKey(polling.BudgetSourceYouTubeScraper, polling.BudgetBurstPrimary)))

	third, decision, err := limiter.TryReserve(ctx, testBudgetJob("expires-c"), testBudgetProfile(polling.BudgetSourceYouTubeScraper, polling.BudgetBurstPrimary), time.Minute)
	require.NoError(t, err)
	require.Nil(t, third)
	require.False(t, decision.Allowed)
	require.Equal(t, "budget_exhausted", decision.Reason)
	require.Equal(t, 1, testInflightValue(t, ctx, cacheClient, testGlobalInflightKey(polling.BudgetSourceYouTubeScraper)))
	require.Equal(t, 1, testInflightValue(t, ctx, cacheClient, testClassInflightKey(polling.BudgetSourceYouTubeScraper, polling.BudgetBurstPrimary)))
}

func TestGlobalBudgetLimiterStoresReservationHashAtEncodedMemberKey(t *testing.T) {
	ctx := context.Background()
	cacheClient := sharedtestutil.NewTestCacheService(t, ctx)
	limiter := newTestGlobalBudgetLimiter(t, cacheClient, GlobalBudgetLimiterConfig{
		SourceMaxInflight: map[polling.BudgetSource]int{polling.BudgetSourceYouTubeScraper: 5},
		ClassMaxInflight:  map[polling.BudgetBurstClass]int{polling.BudgetBurstBackfill: 5},
	})

	held, decision, err := limiter.TryReserve(ctx, testBudgetJob("encoded-member-hash"), testBudgetProfile(polling.BudgetSourceYouTubeScraper, polling.BudgetBurstBackfill), time.Minute)
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.NotNil(t, held)
	reservation, ok := held.(*globalBudgetReservation)
	require.True(t, ok)

	member := string(polling.BudgetBurstBackfill) + "|" + reservation.ownerToken
	keys := buildGlobalBudgetKeys(testBudgetNamespace, polling.BudgetSourceYouTubeScraper, polling.BudgetBurstBackfill, member)
	require.True(t, testKeyExists(t, ctx, cacheClient, keys.Reservation), "encoded reservation hash must use the encoded member key")
}

func TestGlobalBudgetLimiterPreservesNoTTLSharedKeysWhenSettingReservationTTL(t *testing.T) {
	ctx := context.Background()
	cacheClient := sharedtestutil.NewTestCacheService(t, ctx)
	source := polling.BudgetSourceYouTubeScraper
	class := polling.BudgetBurstPrimary
	keys := buildGlobalBudgetKeys(testBudgetNamespace, source, class, "legacy")
	require.NoError(t, cacheClient.GetClient().Do(ctx, cacheClient.B().Set().Key(keys.ClassInflight).Value("0").Build()).Error())
	require.NoError(t, cacheClient.GetClient().Do(ctx, cacheClient.B().Set().Key(keys.GlobalInflight).Value("0").Build()).Error())
	require.NoError(t, cacheClient.GetClient().Do(ctx, cacheClient.B().Zadd().
		Key(keys.Reservations).
		ScoreMember().
		ScoreMember(float64(time.Now().Add(time.Hour).UnixMilli()), "legacy").
		Build()).Error())
	limiter := newTestGlobalBudgetLimiter(t, cacheClient, GlobalBudgetLimiterConfig{
		SourceMaxInflight: map[polling.BudgetSource]int{source: 5},
		ClassMaxInflight:  map[polling.BudgetBurstClass]int{class: 5},
	})

	held, decision, err := limiter.TryReserve(ctx, testBudgetJob("ttl-preserve"), testBudgetProfile(source, class), time.Minute)
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.NotNil(t, held)
	reservation, ok := held.(*globalBudgetReservation)
	require.True(t, ok)

	require.Equal(t, int64(-1), testKeyPTTL(t, ctx, cacheClient, keys.ClassInflight))
	require.Equal(t, int64(-1), testKeyPTTL(t, ctx, cacheClient, keys.GlobalInflight))
	require.Equal(t, int64(-1), testKeyPTTL(t, ctx, cacheClient, keys.Reservations))
	reservationMember := string(class) + "|" + reservation.ownerToken
	reservationKeys := buildGlobalBudgetKeys(testBudgetNamespace, source, class, reservationMember)
	require.Greater(t, testKeyPTTL(t, ctx, cacheClient, reservationKeys.Reservation), int64(0))
}

func TestGlobalBudgetLimiterDeniesWhenSourceCooldownPresent(t *testing.T) {
	ctx := context.Background()
	cacheClient := sharedtestutil.NewTestCacheService(t, ctx)
	limiter := newTestGlobalBudgetLimiter(t, cacheClient, GlobalBudgetLimiterConfig{
		SourceMaxInflight: map[polling.BudgetSource]int{polling.BudgetSourceYouTubeScraper: 5},
		DeniedRetryAfter:  7 * time.Second,
	})
	require.NoError(t, cacheClient.GetClient().Do(ctx, cacheClient.B().Set().Key(testSourceCooldownKey(polling.BudgetSourceYouTubeScraper)).Value("1").Build()).Error())

	reservation, decision, err := limiter.TryReserve(ctx, testBudgetJob("cooldown"), testBudgetProfile(polling.BudgetSourceYouTubeScraper, polling.BudgetBurstPrimary), time.Minute)

	require.NoError(t, err)
	require.Nil(t, reservation)
	require.False(t, decision.Allowed)
	require.Equal(t, "source_cooldown", decision.Reason)
	require.Equal(t, 7*time.Second, decision.RetryAfter)
	require.Equal(t, 0, testInflightValue(t, ctx, cacheClient, testGlobalInflightKey(polling.BudgetSourceYouTubeScraper)))
}

func TestGlobalBudgetLimiterAllowsEmptySourceUnitsWithoutValkeyAccess(t *testing.T) {
	ctx := context.Background()
	cacheClient, mini := sharedtestutil.NewTestCacheServiceWithMini(t, ctx)
	limiter := newTestGlobalBudgetLimiter(t, cacheClient, GlobalBudgetLimiterConfig{
		SourceMaxInflight: map[polling.BudgetSource]int{polling.BudgetSourceYouTubeScraper: 1},
	})
	require.NoError(t, cacheClient.Close())
	mini.Close()

	reservation, decision, err := limiter.TryReserve(ctx, testBudgetJob("empty"), polling.BudgetProfile{}, time.Minute)

	require.NoError(t, err)
	require.Nil(t, reservation)
	require.True(t, decision.Allowed)
}

func TestGlobalBudgetLimiterReturnsErrorWhenValkeyUnavailable(t *testing.T) {
	ctx := context.Background()
	cacheClient, mini := sharedtestutil.NewTestCacheServiceWithMini(t, ctx)
	limiter := newTestGlobalBudgetLimiter(t, cacheClient, GlobalBudgetLimiterConfig{
		SourceMaxInflight: map[polling.BudgetSource]int{polling.BudgetSourceYouTubeScraper: 1},
	})
	require.NoError(t, cacheClient.Close())
	mini.Close()

	reservation, decision, err := limiter.TryReserve(ctx, testBudgetJob("closed-cache"), testBudgetProfile(polling.BudgetSourceYouTubeScraper, polling.BudgetBurstPrimary), time.Minute)

	require.Error(t, err)
	require.Nil(t, reservation)
	require.False(t, decision.Allowed)
}

func TestGlobalBudgetLimiterAllowsSourceWithoutConfiguredCap(t *testing.T) {
	ctx := context.Background()
	cacheClient := sharedtestutil.NewTestCacheService(t, ctx)
	limiter := newTestGlobalBudgetLimiter(t, cacheClient, GlobalBudgetLimiterConfig{
		SourceMaxInflight: map[polling.BudgetSource]int{polling.BudgetSourceYouTubeScraper: 1},
		ClassMaxInflight:  map[polling.BudgetBurstClass]int{polling.BudgetBurstPrimary: 0},
	})

	for i := range 3 {
		reservation, decision, err := limiter.TryReserve(ctx, testBudgetJob(fmt.Sprintf("postgres-%d", i)), testBudgetProfile(polling.BudgetSourcePostgresWrite, polling.BudgetBurstPrimary), time.Minute)
		require.NoError(t, err)
		require.True(t, decision.Allowed)
		require.NotNil(t, reservation)
	}
	require.Equal(t, 3, testInflightValue(t, ctx, cacheClient, testGlobalInflightKey(polling.BudgetSourcePostgresWrite)))
	require.Equal(t, 3, testInflightValue(t, ctx, cacheClient, testClassInflightKey(polling.BudgetSourcePostgresWrite, polling.BudgetBurstPrimary)))
}

func TestNewGlobalBudgetLimiterValidatesRequiredConfig(t *testing.T) {
	ctx := context.Background()
	cacheClient := sharedtestutil.NewTestCacheService(t, ctx)

	limiter, err := NewGlobalBudgetLimiter(nil, GlobalBudgetLimiterConfig{Namespace: testBudgetNamespace})
	require.Error(t, err)
	require.Nil(t, limiter)

	limiter, err = NewGlobalBudgetLimiter(cacheClient, GlobalBudgetLimiterConfig{})
	require.Error(t, err)
	require.Nil(t, limiter)
}

func newTestGlobalBudgetLimiter(t *testing.T, cacheClient cache.Client, cfg GlobalBudgetLimiterConfig) polling.GlobalBudgetLimiter {
	t.Helper()
	cfg.Namespace = testBudgetNamespace
	cfg.InstanceID = "ap-a"
	limiter, err := NewGlobalBudgetLimiter(cacheClient, cfg)
	require.NoError(t, err)
	return limiter
}

func testAllowedReservation(t *testing.T, ctx context.Context, limiter polling.GlobalBudgetLimiter, jobKey string) polling.BudgetReservation {
	t.Helper()
	reservation, decision, err := limiter.TryReserve(ctx, testBudgetJob(jobKey), testBudgetProfile(polling.BudgetSourceYouTubeScraper, polling.BudgetBurstPrimary), time.Minute)
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.NotNil(t, reservation)
	return reservation
}

func testBudgetJob(jobKey string) *polling.BudgetJob {
	return &polling.BudgetJob{
		Namespace:  testBudgetNamespace,
		InstanceID: "ap-a",
		PollerName: "videos",
		ChannelID:  "UC_TEST",
		JobKey:     jobKey,
	}
}

func testBudgetProfile(source polling.BudgetSource, class polling.BudgetBurstClass) polling.BudgetProfile {
	return polling.BudgetProfile{
		SourceUnits: map[polling.BudgetSource]float64{source: 3.5},
		BurstClass:  class,
		Priority:    polling.BudgetPriorityNormal,
	}
}

func testInflightValue(t *testing.T, ctx context.Context, cacheClient *cache.Service, key string) int {
	t.Helper()
	value, hit, err := cacheClient.GetString(ctx, key)
	require.NoError(t, err)
	if !hit {
		return 0
	}
	parsed, err := strconv.Atoi(value)
	require.NoError(t, err)
	return parsed
}

func testKeyExists(t *testing.T, ctx context.Context, cacheClient *cache.Service, key string) bool {
	t.Helper()
	exists, err := cacheClient.GetClient().Do(ctx, cacheClient.B().Exists().Key(key).Build()).AsInt64()
	require.NoError(t, err)
	return exists > 0
}

func testKeyPTTL(t *testing.T, ctx context.Context, cacheClient *cache.Service, key string) int64 {
	t.Helper()
	ttl, err := cacheClient.GetClient().Do(ctx, cacheClient.B().Pttl().Key(key).Build()).AsInt64()
	require.NoError(t, err)
	return ttl
}

func testClassInflightKey(source polling.BudgetSource, class polling.BudgetBurstClass) string {
	return fmt.Sprintf("hololive:%s:youtube-producer:budget:{%s}:%s:inflight", testBudgetNamespace, source, class)
}

func testGlobalInflightKey(source polling.BudgetSource) string {
	return fmt.Sprintf("hololive:%s:youtube-producer:budget:{%s}:global:inflight", testBudgetNamespace, source)
}

func testSourceCooldownKey(source polling.BudgetSource) string {
	return fmt.Sprintf("hololive:%s:youtube-producer:source-cooldown:{%s}", testBudgetNamespace, source)
}

func TestGlobalBudgetLimiterReleaseRestoresBackfillClassInflight(t *testing.T) {
	ctx := context.Background()
	cacheClient := sharedtestutil.NewTestCacheService(t, ctx)
	limiter := newTestGlobalBudgetLimiter(t, cacheClient, GlobalBudgetLimiterConfig{
		SourceMaxInflight: map[polling.BudgetSource]int{polling.BudgetSourceYouTubeScraper: 5},
		ClassMaxInflight:  map[polling.BudgetBurstClass]int{polling.BudgetBurstBackfill: 1},
	})

	held, decision, err := limiter.TryReserve(ctx, testBudgetJob("backfill-release"), testBudgetProfile(polling.BudgetSourceYouTubeScraper, polling.BudgetBurstBackfill), time.Minute)
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.NotNil(t, held)
	require.Equal(t, 1, testInflightValue(t, ctx, cacheClient, testClassInflightKey(polling.BudgetSourceYouTubeScraper, polling.BudgetBurstBackfill)))
	require.Equal(t, 1, testInflightValue(t, ctx, cacheClient, testGlobalInflightKey(polling.BudgetSourceYouTubeScraper)))

	require.NoError(t, held.Release(ctx))
	require.Equal(t, 0, testInflightValue(t, ctx, cacheClient, testClassInflightKey(polling.BudgetSourceYouTubeScraper, polling.BudgetBurstBackfill)))
	require.Equal(t, 0, testInflightValue(t, ctx, cacheClient, testGlobalInflightKey(polling.BudgetSourceYouTubeScraper)))

	again, decision, err := limiter.TryReserve(ctx, testBudgetJob("backfill-release-again"), testBudgetProfile(polling.BudgetSourceYouTubeScraper, polling.BudgetBurstBackfill), time.Minute)
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.NotNil(t, again)
}

func TestGlobalBudgetLimiterRollbackSucceedsWithCanceledContext(t *testing.T) {
	ctx := context.Background()
	cacheClient := sharedtestutil.NewTestCacheService(t, ctx)
	limiter := newTestGlobalBudgetLimiter(t, cacheClient, GlobalBudgetLimiterConfig{
		SourceMaxInflight: map[polling.BudgetSource]int{polling.BudgetSourceHolodexLive: 2},
	})

	held, decision, err := limiter.TryReserve(ctx, testBudgetJob("rollback-canceled-ctx"), testBudgetProfile(polling.BudgetSourceHolodexLive, polling.BudgetBurstPrimary), time.Minute)
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.NotNil(t, held)
	require.Equal(t, 1, testInflightValue(t, ctx, cacheClient, testGlobalInflightKey(polling.BudgetSourceHolodexLive)))

	reservation, ok := held.(*globalBudgetReservation)
	require.True(t, ok)
	inner, ok := limiter.(*globalBudgetLimiter)
	require.True(t, ok)

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	require.NoError(t, inner.releaseSourcesForClass(canceledCtx, reservation.ownerToken, reservation.reservationMember, reservation.burstClass, reservation.sources))
	require.Equal(t, 0, testInflightValue(t, ctx, cacheClient, testGlobalInflightKey(polling.BudgetSourceHolodexLive)))
	require.Equal(t, 0, testInflightValue(t, ctx, cacheClient, testClassInflightKey(polling.BudgetSourceHolodexLive, polling.BudgetBurstPrimary)))
}

const testBudgetNamespace = "test"
