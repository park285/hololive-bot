package alarm

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

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
	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		lookedUpKey = key
		return []string{"room-a", "room-b"}, nil
	}

	got, err := LookupChannelSubscribersByType(t.Context(), cacheSvc, "UC_shorts", domain.AlarmTypeShorts)
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
	requireAlarmRecord(t, db, domain.Alarm{
		RoomID:     "room-db",
		ChannelID:  "UC_shorts",
		AlarmTypes: domain.AlarmTypes{domain.AlarmTypeShorts},
	})

	warmed := make(map[string][]string)
	cacheSvc := cachemocks.NewLenientClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key != sharedalarmkeys.BuildChannelSubscriberKey("UC_shorts", domain.AlarmTypeShorts) {
			t.Fatalf("unexpected cache lookup key %q", key)
		}
		return nil, nil
	}
	cacheSvc.SAddFunc = func(_ context.Context, key string, members []string) (int64, error) {
		warmed[key] = append(warmed[key], members...)
		return int64(len(members)), nil
	}

	got, err := ResolveChannelSubscribersByType(t.Context(), cacheSvc, db, "UC_shorts", domain.AlarmTypeShorts)
	if err != nil {
		t.Fatalf("ResolveChannelSubscribersByType() error = %v", err)
	}
	if len(got) != 1 || got[0] != "room-db" {
		t.Fatalf("ResolveChannelSubscribersByType() = %#v", got)
	}

	typedKey := sharedalarmkeys.BuildChannelSubscriberKey("UC_shorts", domain.AlarmTypeShorts)
	if len(warmed[typedKey]) != 1 || warmed[typedKey][0] != "room-db" {
		t.Fatalf("typed cache warm = %#v", warmed[typedKey])
	}
}

func TestResolveChannelSubscribersByTypeFallsBackToDBWhenCacheErrors(t *testing.T) {
	t.Parallel()

	db := newAlarmTargetLookupTestDB(t)
	requireAlarmRecord(t, db, domain.Alarm{
		RoomID:     "room-db",
		ChannelID:  "UC_community",
		AlarmTypes: domain.AlarmTypes{domain.AlarmTypeCommunity},
	})

	cacheSvc := cachemocks.NewLenientClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key != sharedalarmkeys.BuildChannelSubscriberKey("UC_community", domain.AlarmTypeCommunity) {
			t.Fatalf("unexpected cache lookup key %q", key)
		}
		return nil, errors.New("cache unavailable")
	}

	got, err := ResolveChannelSubscribersByType(t.Context(), cacheSvc, db, "UC_community", domain.AlarmTypeCommunity)
	if err != nil {
		t.Fatalf("ResolveChannelSubscribersByType() error = %v", err)
	}
	if len(got) != 1 || got[0] != "room-db" {
		t.Fatalf("ResolveChannelSubscribersByType() = %#v", got)
	}
}

func TestResolveChannelSubscribersByTypeReturnsAuthoritativeEmptyOnlyAfterDBFallback(t *testing.T) {
	t.Parallel()

	db := newAlarmTargetLookupTestDB(t)
	cacheSvc := cachemocks.NewLenientClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key != sharedalarmkeys.BuildChannelSubscriberKey("UC_empty", domain.AlarmTypeLive) {
			t.Fatalf("unexpected cache lookup key %q", key)
		}
		return nil, nil
	}

	got, err := ResolveChannelSubscribersByType(t.Context(), cacheSvc, db, "UC_empty", domain.AlarmTypeLive)
	if err != nil {
		t.Fatalf("ResolveChannelSubscribersByType() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ResolveChannelSubscribersByType() = %#v", got)
	}
}

func TestResolveChannelSubscribersByTypeUsesNegativeCacheForAuthoritativeEmpty(t *testing.T) {
	t.Parallel()

	emptyKey := sharedalarmkeys.BuildChannelSubscriberEmptyKey("UC_empty", domain.AlarmTypeLive)
	emptyKnown := false

	cacheSvc := cachemocks.NewLenientClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key != sharedalarmkeys.BuildChannelSubscriberKey("UC_empty", domain.AlarmTypeLive) {
			t.Fatalf("unexpected cache lookup key %q", key)
		}
		return nil, nil
	}
	cacheSvc.ExistsFunc = func(_ context.Context, key string) (bool, error) {
		if key != emptyKey {
			t.Fatalf("unexpected exists key %q", key)
		}
		return emptyKnown, nil
	}
	cacheSvc.SetFunc = func(_ context.Context, key string, value any, _ time.Duration) error {
		if key != emptyKey {
			t.Fatalf("unexpected set key %q", key)
		}
		if value != "1" {
			t.Fatalf("unexpected set value %#v", value)
		}
		emptyKnown = true
		return nil
	}

	db := newAlarmTargetLookupTestDB(t)
	got, err := ResolveChannelSubscribersByType(t.Context(), cacheSvc, db, "UC_empty", domain.AlarmTypeLive)
	if err != nil {
		t.Fatalf("ResolveChannelSubscribersByType() first error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ResolveChannelSubscribersByType() first = %#v", got)
	}

	got, err = ResolveChannelSubscribersByType(t.Context(), cacheSvc, nil, "UC_empty", domain.AlarmTypeLive)
	if err != nil {
		t.Fatalf("ResolveChannelSubscribersByType() second error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ResolveChannelSubscribersByType() second = %#v", got)
	}
}

func TestResolveChannelSubscribersByType_SingleflightDeduplicatesConcurrentDBFallback(t *testing.T) {
	t.Parallel()

	db := newAlarmTargetLookupTestDB(t)
	requireAlarmRecord(t, db, domain.Alarm{
		RoomID:     "room-shared",
		ChannelID:  "UC_batch_channel",
		AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive, domain.AlarmTypeShorts, domain.AlarmTypeCommunity},
	})

	var queryCount atomic.Int32
	registerAlarmQueryHook(t, db, func() {
		queryCount.Add(1)
		time.Sleep(50 * time.Millisecond)
	})

	alarmTypes := []domain.AlarmType{
		domain.AlarmTypeLive,
		domain.AlarmTypeShorts,
		domain.AlarmTypeCommunity,
	}

	start := make(chan struct{})
	type result struct {
		subscribers []string
		err         error
	}

	results := make([]result, len(alarmTypes))
	var wg sync.WaitGroup
	for i, alarmType := range alarmTypes {
		wg.Add(1)
		go func(index int, alarmType domain.AlarmType) {
			defer wg.Done()
			<-start
			results[index].subscribers, results[index].err = ResolveChannelSubscribersByType(
				t.Context(),
				nil,
				db,
				"UC_batch_channel",
				alarmType,
			)
		}(i, alarmType)
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
	requireAlarmRecord(t, db, domain.Alarm{
		RoomID:     "room-original",
		ChannelID:  "UC_pointer_channel",
		MemberName: "Original Member",
		AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive, domain.AlarmTypeShorts},
	})

	registerAlarmQueryHook(t, db, func() {
		time.Sleep(50 * time.Millisecond)
	})

	start := make(chan struct{})
	var first []*domain.Alarm
	var second []*domain.Alarm
	var firstErr error
	var secondErr error

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		first, firstErr = loadChannelSubscriberAlarms(t.Context(), db, "UC_pointer_channel")
	}()
	go func() {
		defer wg.Done()
		<-start
		second, secondErr = loadChannelSubscriberAlarms(t.Context(), db, "UC_pointer_channel")
	}()

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

func TestLoadChannelSubscriberAlarms_QueryContextPreservesParentDeadline(t *testing.T) {
	t.Parallel()

	db := newAlarmTargetLookupTestDB(t)
	deadline := time.Now().Add(200 * time.Millisecond)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	deadlines := make(chan time.Time, 1)
	hasDeadline := make(chan bool, 1)
	registerAlarmQueryTxHook(t, db, func(tx *gorm.DB) {
		capturedDeadline, ok := tx.Statement.Context.Deadline()
		hasDeadline <- ok
		if ok {
			deadlines <- capturedDeadline
		}
	})

	alarms, err := loadChannelSubscriberAlarms(ctx, db, "UC_deadline_preserved")
	require.NoError(t, err)
	require.Nil(t, alarms)
	require.True(t, <-hasDeadline)
	require.WithinDuration(t, deadline, <-deadlines, 5*time.Millisecond)
}

func TestLoadChannelSubscriberAlarms_QueryContextAppliesFallbackTimeoutWithoutParentDeadline(t *testing.T) {
	t.Parallel()

	db := newAlarmTargetLookupTestDB(t)

	deadlines := make(chan time.Time, 1)
	hasDeadline := make(chan bool, 1)
	registerAlarmQueryTxHook(t, db, func(tx *gorm.DB) {
		capturedDeadline, ok := tx.Statement.Context.Deadline()
		hasDeadline <- ok
		if ok {
			deadlines <- capturedDeadline
		}
	})

	alarms, err := loadChannelSubscriberAlarms(context.Background(), db, "UC_fallback_timeout")
	require.NoError(t, err)
	require.Nil(t, alarms)
	require.True(t, <-hasDeadline)

	remaining := time.Until(<-deadlines)
	require.Greater(t, remaining, 4*time.Second)
	require.Less(t, remaining, 6*time.Second)
}

func TestLoadChannelSubscriberAlarms_SingleflightSharesDeadlineBoundQuery(t *testing.T) {
	t.Parallel()

	db := newAlarmTargetLookupTestDB(t)
	requireAlarmRecord(t, db, domain.Alarm{
		RoomID:     "room-mixed",
		ChannelID:  "UC_mixed_context",
		AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive},
	})

	var queryCount atomic.Int32
	queryStarted := make(chan struct{})
	releaseQuery := make(chan struct{})
	registerAlarmQueryTxHook(t, db, func(tx *gorm.DB) {
		queryCount.Add(1)
		select {
		case <-queryStarted:
		default:
			close(queryStarted)
		}

		select {
		case <-tx.Statement.Context.Done():
		case <-releaseQuery:
		}
	})

	shortCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	firstDone := make(chan alarmLoadResult, 1)
	go func() {
		alarms, err := loadChannelSubscriberAlarms(shortCtx, db, "UC_mixed_context")
		firstDone <- alarmLoadResult{alarms: alarms, err: err}
	}()

	<-queryStarted

	secondDone := make(chan alarmLoadResult, 1)
	go func() {
		alarms, err := loadChannelSubscriberAlarms(context.Background(), db, "UC_mixed_context")
		secondDone <- alarmLoadResult{alarms: alarms, err: err}
	}()

	first := waitAlarmLoadResult(t, firstDone, 250*time.Millisecond, releaseQuery, "first")
	require.Error(t, first.err)
	assert.ErrorContains(t, first.err, context.DeadlineExceeded.Error())

	second := waitAlarmLoadResult(t, secondDone, 250*time.Millisecond, releaseQuery, "second")
	require.Error(t, second.err)
	assert.ErrorContains(t, second.err, context.DeadlineExceeded.Error())

	if got := queryCount.Load(); got != 1 {
		t.Fatalf("db query count = %d, want 1", got)
	}
}

func TestResolveChannelSubscribersByType_DBFallbackMetrics(t *testing.T) {
	db := newAlarmTargetLookupTestDB(t)
	requireAlarmRecord(t, db, domain.Alarm{
		RoomID:     "room-hit",
		ChannelID:  "UC_metric",
		AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive},
	})

	hitBefore := testutil.ToFloat64(alarmSubscriberDBFallbackTotal.WithLabelValues("hit"))
	_, err := ResolveChannelSubscribersByType(t.Context(), nil, db, "UC_metric", domain.AlarmTypeLive)
	require.NoError(t, err)
	hitAfter := testutil.ToFloat64(alarmSubscriberDBFallbackTotal.WithLabelValues("hit"))
	assert.Equal(t, float64(1), hitAfter-hitBefore)

	missBefore := testutil.ToFloat64(alarmSubscriberDBFallbackTotal.WithLabelValues("miss"))
	_, err = ResolveChannelSubscribersByType(t.Context(), nil, db, "UC_metric", domain.AlarmTypeCommunity)
	require.NoError(t, err)
	missAfter := testutil.ToFloat64(alarmSubscriberDBFallbackTotal.WithLabelValues("miss"))
	assert.Equal(t, float64(1), missAfter-missBefore)

	errorBefore := testutil.ToFloat64(alarmSubscriberDBFallbackTotal.WithLabelValues("error"))
	_, err = ResolveChannelSubscribersByType(t.Context(), nil, nil, "UC_metric", domain.AlarmTypeLive)
	require.Error(t, err)
	errorAfter := testutil.ToFloat64(alarmSubscriberDBFallbackTotal.WithLabelValues("error"))
	assert.Equal(t, float64(1), errorAfter-errorBefore)
}

func TestLoadChannelSubscriberAlarms_SingleflightSharedMetric(t *testing.T) {
	db := newAlarmTargetLookupTestDB(t)
	requireAlarmRecord(t, db, domain.Alarm{
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
	for range 2 {
		go func() {
			<-start
			_, err := loadChannelSubscriberAlarms(t.Context(), db, "UC_shared_metric")
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

func newAlarmTargetLookupTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	if err := db.AutoMigrate(&domain.Alarm{}); err != nil {
		t.Fatalf("migrate alarms: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB(): %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	return db
}

func requireAlarmRecord(t *testing.T, db *gorm.DB, alarmRecord domain.Alarm) {
	t.Helper()

	if err := db.Create(&alarmRecord).Error; err != nil {
		t.Fatalf("create alarm record: %v", err)
	}
}

func registerAlarmQueryHook(t *testing.T, db *gorm.DB, onQuery func()) {
	t.Helper()

	registerAlarmQueryTxHook(t, db, func(_ *gorm.DB) {
		onQuery()
	})
}

func registerAlarmQueryTxHook(t *testing.T, db *gorm.DB, onQuery func(tx *gorm.DB)) {
	t.Helper()

	callbackName := fmt.Sprintf("test:alarm-query-hook:%s", t.Name())
	if err := db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		onQuery(tx)
	}); err != nil {
		t.Fatalf("register query hook: %v", err)
	}

	t.Cleanup(func() {
		if err := db.Callback().Query().Remove(callbackName); err != nil {
			t.Fatalf("remove query hook: %v", err)
		}
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
