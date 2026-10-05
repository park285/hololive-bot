package alarm

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

type alarmLoadResult struct {
	alarms []*domain.Alarm
	err    error
}

func TestLookupChannelSubscribersByTypeUsesTypedKey(t *testing.T) {
	t.Parallel()

	var lookedUpKey string

	cache := cachemocks.NewStrictClient()

	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		lookedUpKey = key
		return []string{"room-a", "room-b"}, nil
	}

	got, err := LookupChannelSubscribersByType(t.Context(), cache, "UC_shorts", domain.AlarmTypeShorts)
	if err != nil {
		t.Fatalf("LookupChannelSubscribersByType() error = %v", err)
	}

	wantKey := sharedalarmkeys.BuildChannelSubscriberKey("UC_shorts", domain.AlarmTypeShorts)
	if lookedUpKey != wantKey {
		t.Fatalf("lookup key = %q, want %q", lookedUpKey, wantKey)
	}

	if len(got) != 2 || got[0] != "room-a" || got[1] != "room-b" {
		t.Fatalf("LookupChannelSubscribersByType() = %#v", got)
	}
}

func TestResolveChannelSubscribersByTypeFallsBackToDBWhenCacheEmpty(t *testing.T) {
	t.Parallel()

	db := newAlarmTargetLookupTestDB(t)
	requireAlarmRecord(t, db, &domain.Alarm{
		RoomID:     testDBRoomID,
		ChannelID:  "UC_shorts",
		AlarmTypes: domain.AlarmTypes{domain.AlarmTypeShorts},
	})

	warmed := make(map[string][]string)
	cache := cachemocks.NewLenientClient()

	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key != sharedalarmkeys.BuildChannelSubscriberKey("UC_shorts", domain.AlarmTypeShorts) {
			t.Fatalf("unexpected cache lookup key %q", key)
		}

		return nil, nil
	}
	cache.SAddFunc = func(_ context.Context, key string, members []string) (int64, error) {
		warmed[key] = append(warmed[key], members...)
		return int64(len(members)), nil
	}

	got, err := NewSubscriberResolver(cache, db).ResolveChannelSubscribersByType(t.Context(), "UC_shorts", domain.AlarmTypeShorts)
	if err != nil {
		t.Fatalf("ResolveChannelSubscribersByType() error = %v", err)
	}

	if len(got) != 1 || got[0] != testDBRoomID {
		t.Fatalf("ResolveChannelSubscribersByType() = %#v", got)
	}

	typedKey := sharedalarmkeys.BuildChannelSubscriberKey("UC_shorts", domain.AlarmTypeShorts)
	if len(warmed[typedKey]) != 0 {
		t.Fatalf("typed cache warm = %#v", warmed[typedKey])
	}
}

func TestResolveChannelSubscribersByType_DoesNotPoisonOtherTypeCacheFromDBFallback(t *testing.T) {
	t.Parallel()

	db := newAlarmTargetLookupTestDB(t)
	requireAlarmRecord(t, db, &domain.Alarm{
		RoomID:     "room-both",
		ChannelID:  "UC_mixed_cache",
		AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive, domain.AlarmTypeCommunity},
	})
	requireAlarmRecord(t, db, &domain.Alarm{
		RoomID:     testCommunityRoomID,
		ChannelID:  "UC_mixed_cache",
		AlarmTypes: domain.AlarmTypes{domain.AlarmTypeCommunity},
	})

	sets := make(map[string]map[string]struct{})
	cache := cachemocks.NewLenientClient()

	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		members := sets[key]
		if len(members) == 0 {
			return nil, nil
		}

		result := make([]string, 0, len(members))
		for member := range members {
			result = append(result, member)
		}

		return result, nil
	}
	cache.SAddFunc = func(_ context.Context, key string, members []string) (int64, error) {
		if sets[key] == nil {
			sets[key] = make(map[string]struct{}, len(members))
		}

		for _, member := range members {
			sets[key][member] = struct{}{}
		}

		return int64(len(members)), nil
	}

	resolver := NewSubscriberResolver(cache, db)

	liveSubscribers, err := resolver.ResolveChannelSubscribersByType(t.Context(), "UC_mixed_cache", domain.AlarmTypeLive)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"room-both"}, liveSubscribers)

	communitySubscribers, err := resolver.ResolveChannelSubscribersByType(t.Context(), "UC_mixed_cache", domain.AlarmTypeCommunity)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"room-both", testCommunityRoomID}, communitySubscribers)
}

func TestResolveChannelSubscribersByTypeFallsBackToDBWhenCacheErrors(t *testing.T) {
	t.Parallel()

	db := newAlarmTargetLookupTestDB(t)
	requireAlarmRecord(t, db, &domain.Alarm{
		RoomID:     testDBRoomID,
		ChannelID:  "UC_community",
		AlarmTypes: domain.AlarmTypes{domain.AlarmTypeCommunity},
	})

	cache := cachemocks.NewLenientClient()

	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key != sharedalarmkeys.BuildChannelSubscriberKey("UC_community", domain.AlarmTypeCommunity) {
			t.Fatalf("unexpected cache lookup key %q", key)
		}

		return nil, errors.New("cache unavailable")
	}

	got, err := NewSubscriberResolver(cache, db).ResolveChannelSubscribersByType(t.Context(), "UC_community", domain.AlarmTypeCommunity)
	if err != nil {
		t.Fatalf("ResolveChannelSubscribersByType() error = %v", err)
	}

	if len(got) != 1 || got[0] != testDBRoomID {
		t.Fatalf("ResolveChannelSubscribersByType() = %#v", got)
	}
}

func TestResolveChannelSubscribersByTypeReturnsAuthoritativeEmptyOnlyAfterDBFallback(t *testing.T) {
	t.Parallel()

	db := newAlarmTargetLookupTestDB(t)
	cache := cachemocks.NewLenientClient()

	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key != sharedalarmkeys.BuildChannelSubscriberKey("UC_empty", domain.AlarmTypeLive) {
			t.Fatalf("unexpected cache lookup key %q", key)
		}

		return nil, nil
	}

	got, err := NewSubscriberResolver(cache, db).ResolveChannelSubscribersByType(t.Context(), "UC_empty", domain.AlarmTypeLive)
	if err != nil {
		t.Fatalf("ResolveChannelSubscribersByType() error = %v", err)
	}

	if len(got) != 0 {
		t.Fatalf("ResolveChannelSubscribersByType() = %#v", got)
	}
}

func TestResolveChannelSubscribersByTypeUsesExplicitNegativeCache(t *testing.T) {
	t.Parallel()

	cache := cachemocks.NewLenientClient()

	cache.ExistsFunc = func(_ context.Context, key string) (bool, error) {
		require.Equal(t, sharedalarmkeys.BuildChannelSubscriberEmptyKey("UC_empty", domain.AlarmTypeLive), key)

		return true, nil
	}

	got, err := NewSubscriberResolver(cache, nil).ResolveChannelSubscribersByType(t.Context(), "UC_empty", domain.AlarmTypeLive)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestResolveChannelSubscribersByType_SingleflightDeduplicatesConcurrentDBFallback(t *testing.T) {
	t.Parallel()

	db := newAlarmTargetLookupTestDB(t)
	requireAlarmRecord(t, db, &domain.Alarm{
		RoomID:     "room-shared",
		ChannelID:  "UC_batch_channel",
		AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive, domain.AlarmTypeShorts, domain.AlarmTypeCommunity},
	})

	var queryCount atomic.Int32

	registerAlarmQueryHook(t, db, func() {
		queryCount.Add(1)
		time.Sleep(50 * time.Millisecond)
	})

	start := make(chan struct{})

	type result struct {
		subscribers []string
		err         error
	}

	const concurrentCalls = 3

	results := make([]result, concurrentCalls)
	resolver := NewSubscriberResolver(nil, db)

	var wg sync.WaitGroup

	for i := range concurrentCalls {
		wg.Go(func() {
			<-start

			results[i].subscribers, results[i].err = resolver.ResolveChannelSubscribersByType(
				t.Context(),
				"UC_batch_channel",
				domain.AlarmTypeShorts,
			)
		})
	}

	close(start)
	wg.Wait()

	for i, result := range results {
		if result.err != nil {
			t.Fatalf("call %d error = %v", i, result.err)
		}

		if len(result.subscribers) != 1 || result.subscribers[0] != "room-shared" {
			t.Fatalf("call %d subscribers = %#v", i, result.subscribers)
		}
	}

	if got := queryCount.Load(); got != 1 {
		t.Fatalf("db query count = %d, want 1", got)
	}
}

func TestLoadChannelSubscriberAlarms_SingleflightDoesNotShareMutablePointers(t *testing.T) {
	t.Parallel()

	db := newAlarmTargetLookupTestDB(t)
	requireAlarmRecord(t, db, &domain.Alarm{
		RoomID:     "room-original",
		ChannelID:  "UC_pointer_channel",
		AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive, domain.AlarmTypeShorts},
	})

	registerAlarmQueryHook(t, db, func() {
		time.Sleep(50 * time.Millisecond)
	})

	start := make(chan struct{})

	var (
		first     []*domain.Alarm
		second    []*domain.Alarm
		firstErr  error
		secondErr error
	)

	var wg sync.WaitGroup

	resolver := NewSubscriberResolver(nil, db)

	wg.Go(func() {
		<-start

		first, firstErr = resolver.loadChannelSubscriberAlarms(t.Context(), "UC_pointer_channel", domain.AlarmTypeLive)
	})
	wg.Go(func() {
		<-start

		second, secondErr = resolver.loadChannelSubscriberAlarms(t.Context(), "UC_pointer_channel", domain.AlarmTypeLive)
	})

	close(start)
	wg.Wait()

	if firstErr != nil {
		t.Fatalf("first loadChannelSubscriberAlarms() error = %v", firstErr)
	}

	if secondErr != nil {
		t.Fatalf("second loadChannelSubscriberAlarms() error = %v", secondErr)
	}

	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("unexpected alarm lengths: first=%d second=%d", len(first), len(second))
	}

	first[0].AlarmTypes[0] = domain.AlarmTypeCommunity

	if len(second[0].AlarmTypes) != 2 {
		t.Fatalf("second AlarmTypes length = %d, want 2", len(second[0].AlarmTypes))
	}

	if second[0].AlarmTypes[0] != domain.AlarmTypeLive {
		t.Fatalf("second AlarmTypes[0] = %q, want %q", second[0].AlarmTypes[0], domain.AlarmTypeLive)
	}

	if second[0].AlarmTypes[1] != domain.AlarmTypeShorts {
		t.Fatalf("second AlarmTypes[1] = %q, want %q", second[0].AlarmTypes[1], domain.AlarmTypeShorts)
	}
}

func TestLoadChannelSubscriberAlarms_QueryContextIgnoresParentDeadline(t *testing.T) {
	t.Parallel()

	testChannelSubscriberQueryContext(t, "UC_deadline_isolated", true)
}

func TestLoadChannelSubscriberAlarms_QueryContextAppliesFallbackTimeoutWithoutParentDeadline(t *testing.T) {
	t.Parallel()

	testChannelSubscriberQueryContext(t, "UC_fallback_timeout", false)
}

// 실제 조회 경로의 Query에 전달된 context를 제어하고, DB 왕복과 호스트 부하 없이 종료 경계를 검증한다.
func testChannelSubscriberQueryContext(t *testing.T, channelID string, parentDeadline bool) {
	t.Helper()

	synctest.Test(t, func(t *testing.T) {
		type queryContextKey struct{}

		value := new(int)
		valueCtx := context.WithValue(t.Context(), queryContextKey{}, value)

		var (
			parent        context.Context
			cancel        context.CancelFunc
			wantParentErr = context.Canceled
		)

		if parentDeadline {
			parent, cancel = context.WithTimeout(valueCtx, 2*time.Second)
			wantParentErr = context.DeadlineExceeded
		} else {
			parent, cancel = context.WithCancel(valueCtx)

			_, ok := parent.Deadline()
			require.False(t, ok)
		}

		defer cancel()

		db := &alarmQueryContextTestDB{entered: make(chan context.Context, 1)}
		resolver := NewSubscriberResolver(nil, db)
		startedAt := time.Now()
		firstDone := startAlarmQueryContextLoad(parent, resolver, channelID)

		queryCtx := <-db.entered
		deadline, ok := queryCtx.Deadline()
		require.True(t, ok)
		assert.Equal(t, startedAt.Add(5*time.Second), deadline)
		assert.NotEqual(t, parent.Done(), queryCtx.Done())
		assert.Same(t, value, queryCtx.Value(queryContextKey{}))

		followerDone := startAlarmQueryContextLoad(t.Context(), resolver, channelID)

		synctest.Wait()

		if parentDeadline {
			synctest.Sleep(2 * time.Second)
		} else {
			cancel()
			synctest.Wait()
		}

		first := <-firstDone
		require.ErrorIs(t, first.err, wantParentErr)
		assert.Nil(t, first.alarms)
		require.NoError(t, queryCtx.Err(), "parent termination must not stop the shared query")
		assert.Equal(t, int32(1), db.calls.Load(), "the follower must share the original query context")

		assertAlarmQueryContextTimeout(queryCtx, t, deadline, followerDone)
		assert.Equal(t, int32(1), db.calls.Load())
	})
}

func startAlarmQueryContextLoad(ctx context.Context, resolver *SubscriberResolver, channelID string) <-chan alarmLoadResult {
	done := make(chan alarmLoadResult, 1)

	go func() {
		alarms, err := resolver.loadChannelSubscriberAlarms(ctx, channelID, domain.AlarmTypeLive)
		done <- alarmLoadResult{alarms: alarms, err: err}
	}()

	return done
}

func assertAlarmQueryContextTimeout(queryCtx context.Context, t *testing.T, deadline time.Time, followerDone <-chan alarmLoadResult) {
	t.Helper()

	synctest.Sleep(time.Until(deadline) - time.Nanosecond)
	require.NoError(t, queryCtx.Err())

	select {
	case result := <-followerDone:
		t.Fatalf("follower finished before the query deadline: %+v", result)
	default:
	}

	synctest.Sleep(time.Nanosecond)
	require.ErrorIs(t, queryCtx.Err(), context.DeadlineExceeded)

	follower := <-followerDone
	require.ErrorIs(t, follower.err, context.DeadlineExceeded)
	assert.Nil(t, follower.alarms)
	assert.Equal(t, deadline, time.Now())
}

type alarmQueryContextTestDB struct {
	entered chan context.Context
	calls   atomic.Int32
}

func (db *alarmQueryContextTestDB) Query(ctx context.Context, _ string, _ ...any) (pgx.Rows, error) {
	db.calls.Add(1)

	db.entered <- ctx

	<-ctx.Done()

	return nil, fmt.Errorf("subscriber query deadline: %w", ctx.Err())
}

func (*alarmQueryContextTestDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("unexpected subscriber query Exec")
}

func (*alarmQueryContextTestDB) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("unexpected subscriber query QueryRow")
}

func TestLoadChannelSubscriberAlarms_SingleflightIsolatesFollowersFromFirstCallerDeadline(t *testing.T) {
	t.Parallel()

	db := newAlarmTargetLookupTestDB(t)
	requireAlarmRecord(t, db, &domain.Alarm{
		RoomID:     "room-mixed",
		ChannelID:  "UC_mixed_context",
		AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive},
	})

	var queryCount atomic.Int32

	queryStarted := make(chan struct{})
	releaseQuery := make(chan struct{})

	registerAlarmQueryTxHook(t, db, func(ctx context.Context) {
		queryCount.Add(1)

		select {
		case <-queryStarted:
		default:
			close(queryStarted)
		}

		select {
		case <-ctx.Done():
		case <-releaseQuery:
		}
	})

	shortCtx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	firstDone := make(chan alarmLoadResult, 1)
	resolver := NewSubscriberResolver(nil, db)

	go func() {
		alarms, err := resolver.loadChannelSubscriberAlarms(shortCtx, "UC_mixed_context", domain.AlarmTypeLive)
		firstDone <- alarmLoadResult{alarms: alarms, err: err}
	}()

	<-queryStarted

	secondDone := make(chan alarmLoadResult, 1)

	go func() {
		alarms, err := resolver.loadChannelSubscriberAlarms(t.Context(), "UC_mixed_context", domain.AlarmTypeLive)
		secondDone <- alarmLoadResult{alarms: alarms, err: err}
	}()

	first := waitAlarmLoadResult(t, firstDone, 250*time.Millisecond, releaseQuery, "first")
	require.Error(t, first.err)
	require.ErrorContains(t, first.err, context.DeadlineExceeded.Error())

	select {
	case <-releaseQuery:
	default:
		close(releaseQuery)
	}

	second := waitAlarmLoadResult(t, secondDone, 250*time.Millisecond, releaseQuery, "second")
	require.NoError(t, second.err)
	require.Len(t, second.alarms, 1)
	assert.Equal(t, "room-mixed", second.alarms[0].RoomID)

	if got := queryCount.Load(); got != 1 {
		t.Fatalf("db query count = %d, want 1", got)
	}
}

func TestResolveChannelSubscribersByType_DBFallbackMetrics(t *testing.T) {
	db := newAlarmTargetLookupTestDB(t)
	requireAlarmRecord(t, db, &domain.Alarm{
		RoomID:     "room-hit",
		ChannelID:  "UC_metric",
		AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive},
	})

	resolver := NewSubscriberResolver(nil, db)

	hitBefore := testutil.ToFloat64(alarmSubscriberDBFallbackTotal.WithLabelValues("hit"))
	_, err := resolver.ResolveChannelSubscribersByType(t.Context(), "UC_metric", domain.AlarmTypeLive)
	require.NoError(t, err)

	hitAfter := testutil.ToFloat64(alarmSubscriberDBFallbackTotal.WithLabelValues("hit"))
	assert.InDelta(t, float64(1), hitAfter-hitBefore, 1e-9)

	missBefore := testutil.ToFloat64(alarmSubscriberDBFallbackTotal.WithLabelValues("miss"))

	_, err = resolver.ResolveChannelSubscribersByType(t.Context(), "UC_metric", domain.AlarmTypeCommunity)
	require.NoError(t, err)

	missAfter := testutil.ToFloat64(alarmSubscriberDBFallbackTotal.WithLabelValues("miss"))
	assert.InDelta(t, float64(1), missAfter-missBefore, 1e-9)

	errorBefore := testutil.ToFloat64(alarmSubscriberDBFallbackTotal.WithLabelValues("error"))

	_, err = NewSubscriberResolver(nil, nil).ResolveChannelSubscribersByType(t.Context(), "UC_metric", domain.AlarmTypeLive)
	require.Error(t, err)

	errorAfter := testutil.ToFloat64(alarmSubscriberDBFallbackTotal.WithLabelValues("error"))
	assert.InDelta(t, float64(1), errorAfter-errorBefore, 1e-9)
}

func TestLoadChannelSubscriberAlarms_SingleflightSharedMetric(t *testing.T) {
	db := newAlarmTargetLookupTestDB(t)
	requireAlarmRecord(t, db, &domain.Alarm{
		RoomID:     "room-shared-metric",
		ChannelID:  "UC_shared_metric",
		AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive},
	})

	releaseQuery := make(chan struct{})

	registerAlarmQueryHook(t, db, func() {
		<-releaseQuery
	})

	start := make(chan struct{})
	done := make(chan error, 2)
	resolver := NewSubscriberResolver(nil, db)

	for range 2 {
		go func() {
			<-start

			_, err := resolver.loadChannelSubscriberAlarms(t.Context(), "UC_shared_metric", domain.AlarmTypeLive)
			done <- err
		}()
	}

	before := testutil.ToFloat64(alarmSubscriberDBSingleflightSharedTotal)

	close(start)
	time.Sleep(20 * time.Millisecond)
	close(releaseQuery)

	require.NoError(t, <-done)
	require.NoError(t, <-done)

	after := testutil.ToFloat64(alarmSubscriberDBSingleflightSharedTotal)
	assert.Greater(t, after-before, float64(0))
}

// 서로 다른 DB(또는 트랜잭션)에 묶인 resolver는 같은 채널·종류를 동시에 조회해도 각자 조회하고 자기 결과만 받아야 한다.
// 한 DB의 실패가 다른 DB 조회자에게 전달되거나 다른 DB 조회가 생략되면 안 된다.
func TestSubscriberResolver_DistinctDatabasesNeverShareLoads(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		dbA := &releasedSubscriberQueryDB{err: errors.New("database A failed"), release: make(chan struct{})}
		dbB := &releasedSubscriberQueryDB{err: errors.New("database B failed"), release: make(chan struct{})}
		resolverA := NewSubscriberResolver(nil, dbA)
		resolverB := NewSubscriberResolver(nil, dbB)

		doneA := make(chan alarmLoadResult, 1)
		doneB := make(chan alarmLoadResult, 1)

		go func() {
			alarms, err := resolverA.loadChannelSubscriberAlarms(t.Context(), "UC_shared_key", domain.AlarmTypeLive)
			doneA <- alarmLoadResult{alarms: alarms, err: err}
		}()
		go func() {
			alarms, err := resolverB.loadChannelSubscriberAlarms(t.Context(), "UC_shared_key", domain.AlarmTypeLive)
			doneB <- alarmLoadResult{alarms: alarms, err: err}
		}()

		synctest.Wait()
		assert.Equal(t, int32(1), dbA.calls.Load())
		assert.Equal(t, int32(1), dbB.calls.Load(), "database B must run its own query while A is in flight")

		close(dbA.release)
		synctest.Wait()

		resultA := <-doneA
		require.ErrorIs(t, resultA.err, dbA.err)
		require.NotErrorIs(t, resultA.err, dbB.err)

		select {
		case result := <-doneB:
			t.Fatalf("database B caller finished with database A result: %+v", result)
		default:
		}

		close(dbB.release)

		resultB := <-doneB
		require.ErrorIs(t, resultB.err, dbB.err)
		require.NotErrorIs(t, resultB.err, dbA.err)
	})
}

// releasedSubscriberQueryDB는 release가 닫힐 때까지 조회를 붙잡은 뒤 고유 오류를 돌려준다.
type releasedSubscriberQueryDB struct {
	err     error
	release chan struct{}
	calls   atomic.Int32
}

func (db *releasedSubscriberQueryDB) Query(ctx context.Context, _ string, _ ...any) (pgx.Rows, error) {
	db.calls.Add(1)

	select {
	case <-db.release:
		return nil, db.err
	case <-ctx.Done():
		return nil, fmt.Errorf("released subscriber query: %w", ctx.Err())
	}
}

func (*releasedSubscriberQueryDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("unexpected subscriber query Exec")
}

func (*releasedSubscriberQueryDB) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("unexpected subscriber query QueryRow")
}

type alarmTargetLookupTestDB struct {
	*pgxpool.Pool

	mu      sync.Mutex
	onQuery func(context.Context)
}

func (db *alarmTargetLookupTestDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	db.mu.Lock()

	onQuery := db.onQuery
	db.mu.Unlock()

	if onQuery != nil {
		onQuery(ctx)
	}

	out, err := db.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}

	return out, nil
}

func (db *alarmTargetLookupTestDB) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	out, err := db.Pool.Exec(ctx, sql, arguments...)
	if err != nil {
		return out, fmt.Errorf("exec: %w", err)
	}

	return out, nil
}

func (db *alarmTargetLookupTestDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return db.Pool.QueryRow(ctx, sql, args...)
}

func newAlarmTargetLookupTestDB(t *testing.T) *alarmTargetLookupTestDB {
	t.Helper()

	return &alarmTargetLookupTestDB{Pool: dbtest.NewPool(t)}
}

func requireAlarmRecord(t *testing.T, db *alarmTargetLookupTestDB, alarmRecord *domain.Alarm) {
	t.Helper()

	alarmTypesValue, err := alarmRecord.AlarmTypes.Value()
	if err != nil {
		t.Fatalf("encode alarm types: %v", err)
	}

	if _, err := db.Exec(t.Context(), `
		INSERT INTO alarms (room_id, user_id, channel_id, room_name, user_name, alarm_types)
		VALUES ($1, $2, $3, $4, $5, $6::alarm_type[])
	`,
		alarmRecord.RoomID,
		alarmRecord.UserID,
		alarmRecord.ChannelID,
		alarmRecord.RoomName,
		alarmRecord.UserName,
		alarmTypesValue,
	); err != nil {
		t.Fatalf("create alarm record: %v", err)
	}
}

func registerAlarmQueryHook(t *testing.T, db *alarmTargetLookupTestDB, onQuery func()) {
	t.Helper()

	registerAlarmQueryTxHook(t, db, func(_ context.Context) {
		onQuery()
	})
}

func registerAlarmQueryTxHook(t *testing.T, db *alarmTargetLookupTestDB, onQuery func(ctx context.Context)) {
	t.Helper()

	db.mu.Lock()

	db.onQuery = onQuery
	db.mu.Unlock()

	t.Cleanup(func() {
		db.mu.Lock()

		db.onQuery = nil
		db.mu.Unlock()
	})
}

func waitAlarmLoadResult(
	t *testing.T,
	results <-chan alarmLoadResult,
	timeout time.Duration,
	releaseQuery chan struct{},
	label string,
) alarmLoadResult {
	t.Helper()

	select {
	case result := <-results:
		return result
	case <-time.After(timeout):
		select {
		case <-releaseQuery:
		default:
			close(releaseQuery)
		}

		t.Fatalf("%s loadChannelSubscriberAlarms() did not return within %s", label, timeout)

		return alarmLoadResult{}
	}
}
