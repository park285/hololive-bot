package polling

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

var seedTwoExpiredBudgetReservationsLua = valkey.NewLuaScript(`
redis.call('SET', KEYS[1], ARGV[1])
redis.call('SET', KEYS[2], ARGV[2])
redis.call('HSET', KEYS[4], 'class', ARGV[3], 'units', '1', 'member', ARGV[4])
redis.call('HSET', KEYS[5], 'class', ARGV[3], 'units', '1', 'member', ARGV[5])
redis.call('ZADD', KEYS[3], ARGV[6], ARGV[4], ARGV[6], ARGV[5])
return 1
`)

var seedHashlessExpiredBudgetReservationLua = valkey.NewLuaScript(`
redis.call('SET', KEYS[1], '1')
redis.call('SET', KEYS[2], '1')
redis.call('ZADD', KEYS[3], ARGV[1], ARGV[2])
return 1
`)

var seedLegacyExpiredBudgetReservationLua = valkey.NewLuaScript(`
redis.call('SET', KEYS[1], '1')
redis.call('SET', KEYS[2], '1')
redis.call('HSET', KEYS[4], 'class', ARGV[1], 'units', '1', 'member', ARGV[2])
redis.call('ZADD', KEYS[3], ARGV[3], ARGV[4])
return 1
`)

func TestGlobalBudgetLimiterBoundedCleanupReportsIncompleteBacklog(t *testing.T) {
	ctx := context.Background()
	cacheClient := sharedtestutil.NewTestCacheService(t, ctx)
	limiter := newTestGlobalBudgetLimiter(t, cacheClient, GlobalBudgetLimiterConfig{
		SourceMaxInflight: map[poller.BudgetSource]int{poller.BudgetSourceYouTubeScraper: 1},
		ClassMaxInflight:  map[poller.BudgetBurstClass]int{poller.BudgetBurstPrimary: 1},
		CleanupLimit:      1,
	})
	seedExpiredBudgetReservations(t, ctx, cacheClient, poller.BudgetSourceYouTubeScraper, poller.BudgetBurstPrimary, "expired-a", "expired-b")

	reservation, decision, err := limiter.TryReserve(ctx, testBudgetJob("bounded-cleanup-a"), testBudgetProfile(poller.BudgetSourceYouTubeScraper, poller.BudgetBurstPrimary), time.Minute)

	require.NoError(t, err)
	require.Nil(t, reservation)
	require.False(t, decision.Allowed)
	require.Equal(t, "budget_cleanup_incomplete", decision.Reason)
	require.Equal(t, 1, testInflightValue(t, ctx, cacheClient, testGlobalInflightKey(poller.BudgetSourceYouTubeScraper)))

	reservation, decision, err = limiter.TryReserve(ctx, testBudgetJob("bounded-cleanup-b"), testBudgetProfile(poller.BudgetSourceYouTubeScraper, poller.BudgetBurstPrimary), time.Minute)

	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.NotNil(t, reservation)
	require.Equal(t, 1, testInflightValue(t, ctx, cacheClient, testGlobalInflightKey(poller.BudgetSourceYouTubeScraper)))
	require.Equal(t, 1, testInflightValue(t, ctx, cacheClient, testClassInflightKey(poller.BudgetSourceYouTubeScraper, poller.BudgetBurstPrimary)))
}

func TestGlobalBudgetLimiterEncodedExpiredMemberCleansEvenWhenHashExpired(t *testing.T) {
	ctx := context.Background()
	cacheClient := sharedtestutil.NewTestCacheService(t, ctx)
	limiter := newTestGlobalBudgetLimiter(t, cacheClient, GlobalBudgetLimiterConfig{
		SourceMaxInflight: map[poller.BudgetSource]int{poller.BudgetSourceYouTubeScraper: 1},
		ClassMaxInflight:  map[poller.BudgetBurstClass]int{poller.BudgetBurstPrimary: 1},
		CleanupLimit:      1,
	})
	seedHashlessExpiredBudgetReservation(t, ctx, cacheClient, poller.BudgetSourceYouTubeScraper, poller.BudgetBurstPrimary, "expired-no-hash")

	reservation, decision, err := limiter.TryReserve(ctx, testBudgetJob("hashless-cleanup"), testBudgetProfile(poller.BudgetSourceYouTubeScraper, poller.BudgetBurstPrimary), time.Minute)

	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.NotNil(t, reservation)
	require.Equal(t, 1, testInflightValue(t, ctx, cacheClient, testGlobalInflightKey(poller.BudgetSourceYouTubeScraper)))
	require.Equal(t, 1, testInflightValue(t, ctx, cacheClient, testClassInflightKey(poller.BudgetSourceYouTubeScraper, poller.BudgetBurstPrimary)))
}

func TestGlobalBudgetLimiterExpiredLegacyMemberFadesOutDuringCleanup(t *testing.T) {
	ctx := context.Background()
	cacheClient := sharedtestutil.NewTestCacheService(t, ctx)
	source := poller.BudgetSourceYouTubeScraper
	class := poller.BudgetBurstPrimary
	token := "expired-legacy-hash"
	limiter := newTestGlobalBudgetLimiter(t, cacheClient, GlobalBudgetLimiterConfig{
		SourceMaxInflight: map[poller.BudgetSource]int{source: 1},
		ClassMaxInflight:  map[poller.BudgetBurstClass]int{class: 1},
		CleanupLimit:      1,
	})
	seedLegacyExpiredBudgetReservation(t, ctx, cacheClient, source, class, token)

	reservation, decision, err := limiter.TryReserve(ctx, testBudgetJob("legacy-fadeout-cleanup"), testBudgetProfile(source, class), time.Minute)

	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.NotNil(t, reservation)
	legacyKeys := buildGlobalBudgetKeys(testBudgetNamespace, source, class, token)
	require.False(t, testKeyExists(t, ctx, cacheClient, legacyKeys.Reservation))
}

func TestGlobalBudgetLimiterReleaseLeavesLegacyOwnerTokenReservationForFadeout(t *testing.T) {
	ctx := context.Background()
	cacheClient := sharedtestutil.NewTestCacheService(t, ctx)
	source := poller.BudgetSourceYouTubeScraper
	class := poller.BudgetBurstBackfill
	limiter := newTestGlobalBudgetLimiter(t, cacheClient, GlobalBudgetLimiterConfig{
		SourceMaxInflight: map[poller.BudgetSource]int{source: 5},
		ClassMaxInflight:  map[poller.BudgetBurstClass]int{class: 5},
	})

	held, decision, err := limiter.TryReserve(ctx, testBudgetJob("release-fadeout"), testBudgetProfile(source, class), time.Minute)
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.NotNil(t, held)
	reservation, ok := held.(*globalBudgetReservation)
	require.True(t, ok)
	legacyKeys := buildGlobalBudgetKeys(testBudgetNamespace, source, class, reservation.ownerToken)
	require.NoError(t, cacheClient.GetClient().Do(ctx, cacheClient.B().Hset().Key(legacyKeys.Reservation).FieldValue().FieldValue("class", string(class)).Build()).Error())
	require.NoError(t, cacheClient.GetClient().Do(ctx, cacheClient.B().Zadd().Key(legacyKeys.Reservations).ScoreMember().ScoreMember(float64(time.Now().Add(time.Minute).UnixMilli()), reservation.ownerToken).Build()).Error())

	require.NoError(t, held.Release(ctx))
	require.True(t, testKeyExists(t, ctx, cacheClient, legacyKeys.Reservation))
	legacyMembers, err := cacheClient.GetClient().Do(ctx, cacheClient.B().Zrange().Key(legacyKeys.Reservations).Min("0").Max("-1").Build()).AsStrSlice()
	require.NoError(t, err)
	require.Contains(t, legacyMembers, reservation.ownerToken)
}

func TestGlobalBudgetLimiterReleaseUsesReservationClassAndMember(t *testing.T) {
	ctx := context.Background()
	cacheClient := sharedtestutil.NewTestCacheService(t, ctx)
	limiter := newTestGlobalBudgetLimiter(t, cacheClient, GlobalBudgetLimiterConfig{
		SourceMaxInflight: map[poller.BudgetSource]int{poller.BudgetSourceYouTubeScraper: 5},
		ClassMaxInflight:  map[poller.BudgetBurstClass]int{poller.BudgetBurstBackfill: 5},
	})

	held, decision, err := limiter.TryReserve(ctx, testBudgetJob("backfill-member-release"), testBudgetProfile(poller.BudgetSourceYouTubeScraper, poller.BudgetBurstBackfill), time.Minute)
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.NotNil(t, held)
	reservation, ok := held.(*globalBudgetReservation)
	require.True(t, ok)
	require.Equal(t, poller.BudgetBurstBackfill, reservation.burstClass)
	require.Contains(t, reservation.reservationMember, globalBudgetReservationMemberSeparator)

	require.NoError(t, held.Release(ctx))
	require.Equal(t, 0, testInflightValue(t, ctx, cacheClient, testGlobalInflightKey(poller.BudgetSourceYouTubeScraper)))
	require.Equal(t, 0, testInflightValue(t, ctx, cacheClient, testClassInflightKey(poller.BudgetSourceYouTubeScraper, poller.BudgetBurstBackfill)))
}

func TestGlobalBudgetReserveScriptHardeningContract(t *testing.T) {
	for _, want := range []string{
		"'LIMIT', 0, cleanupLimit",
		"budget_cleanup_incomplete",
		"function pexpireAtLeast",
		"classForExpiredMember",
		"reservationMember",
	} {
		require.Contains(t, globalBudgetLuaContractText(), want)
	}
	require.NotNil(t, globalBudgetReserveLua)
	require.NotNil(t, globalBudgetReleaseLua)
	require.NotContains(t, globalBudgetReserveScript, "redis.call('PEXPIRE', currentClassKey, reservationTTLMS)")
	require.NotContains(t, globalBudgetReleaseScript, "legacyReservationKey")
	require.NotContains(t, globalBudgetReleaseScript, "reservationMember ~= ownerToken")
}

func seedExpiredBudgetReservations(
	t *testing.T,
	ctx context.Context,
	cacheClient *cache.Service,
	source poller.BudgetSource,
	class poller.BudgetBurstClass,
	firstToken string,
	secondToken string,
) {
	t.Helper()
	firstKeys := buildGlobalBudgetKeys(testBudgetNamespace, source, class, firstToken)
	secondKeys := buildGlobalBudgetKeys(testBudgetNamespace, source, class, secondToken)
	expiredAtMS := time.Now().Add(-time.Second).UnixMilli()
	result := seedTwoExpiredBudgetReservationsLua.Exec(ctx, cacheClient.GetClient(), []string{
		firstKeys.ClassInflight,
		firstKeys.GlobalInflight,
		firstKeys.Reservations,
		firstKeys.Reservation,
		secondKeys.Reservation,
	}, []string{
		"2",
		"2",
		string(class),
		globalBudgetReservationMember(class, firstToken),
		globalBudgetReservationMember(class, secondToken),
		strconv.FormatInt(expiredAtMS, 10),
	})
	require.NoError(t, result.Error())
}

func seedHashlessExpiredBudgetReservation(
	t *testing.T,
	ctx context.Context,
	cacheClient *cache.Service,
	source poller.BudgetSource,
	class poller.BudgetBurstClass,
	token string,
) {
	t.Helper()
	keys := buildGlobalBudgetKeys(testBudgetNamespace, source, class, token)
	expiredAtMS := time.Now().Add(-time.Second).UnixMilli()
	result := seedHashlessExpiredBudgetReservationLua.Exec(ctx, cacheClient.GetClient(), []string{
		keys.ClassInflight,
		keys.GlobalInflight,
		keys.Reservations,
	}, []string{
		strconv.FormatInt(expiredAtMS, 10),
		globalBudgetReservationMember(class, token),
	})
	require.NoError(t, result.Error())
}

func seedLegacyExpiredBudgetReservation(
	t *testing.T,
	ctx context.Context,
	cacheClient *cache.Service,
	source poller.BudgetSource,
	class poller.BudgetBurstClass,
	token string,
) {
	t.Helper()
	keys := buildGlobalBudgetKeys(testBudgetNamespace, source, class, token)
	expiredAtMS := time.Now().Add(-time.Second).UnixMilli()
	result := seedLegacyExpiredBudgetReservationLua.Exec(ctx, cacheClient.GetClient(), []string{
		keys.ClassInflight,
		keys.GlobalInflight,
		keys.Reservations,
		keys.Reservation,
	}, []string{
		string(class),
		token,
		strconv.FormatInt(expiredAtMS, 10),
		token,
	})
	require.NoError(t, result.Error())
}

func globalBudgetLuaContractText() string {
	return globalBudgetReserveScript + "\n" + globalBudgetReleaseScript
}
