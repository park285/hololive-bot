package alarmservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

type stubAlarmWriter struct {
	addFn         func(context.Context, *domain.Alarm) error
	removeFn      func(context.Context, string, string) error
	clearByRoomFn func(context.Context, string) (int64, error)
}

func (w *stubAlarmWriter) Add(ctx context.Context, alarm *domain.Alarm) error {
	if w.addFn != nil {
		if err := w.addFn(ctx, alarm); err != nil {
			return fmt.Errorf("stub add: %w", err)
		}
	}

	return nil
}

func (w *stubAlarmWriter) Remove(ctx context.Context, roomID, channelID string) error {
	if w.removeFn != nil {
		if err := w.removeFn(ctx, roomID, channelID); err != nil {
			return fmt.Errorf("stub remove: %w", err)
		}
	}

	return nil
}

func (w *stubAlarmWriter) ClearByRoom(ctx context.Context, roomID string) (int64, error) {
	if w.clearByRoomFn != nil {
		count, err := w.clearByRoomFn(ctx, roomID)
		if err != nil {
			return 0, fmt.Errorf("stub clear by room: %w", err)
		}

		return count, nil
	}

	return 0, nil
}

func newLenientAlarmCacheMock(
	ctx context.Context,
	t *testing.T,
	sadd func(*cache.Service, context.Context, string, []string) (int64, error),
) (*cachemocks.Client, *cache.Service) {
	t.Helper()

	cacheSvc := newTestCacheService(ctx, t)
	cacheMock := cachemocks.NewLenientClient()

	cacheMock.BuilderFunc = cacheSvc.Builder
	cacheMock.BFunc = cacheSvc.B
	cacheMock.GetClientFunc = cacheSvc.GetClient
	cacheMock.DoMultiFunc = cacheSvc.DoMulti
	cacheMock.SMembersFunc = cacheSvc.SMembers
	cacheMock.SIsMemberFunc = cacheSvc.SIsMember
	cacheMock.HGetFunc = cacheSvc.HGet
	cacheMock.HGetAllFunc = cacheSvc.HGetAll
	cacheMock.SetFunc = cacheSvc.Set
	cacheMock.GetFunc = cacheSvc.Get
	cacheMock.DelFunc = cacheSvc.Del
	cacheMock.DelManyFunc = cacheSvc.DelMany
	cacheMock.ScanKeysFunc = cacheSvc.ScanKeys
	cacheMock.HDelFunc = cacheSvc.HDel
	cacheMock.HMSetFunc = cacheSvc.HMSet
	cacheMock.ExistsFunc = cacheSvc.Exists
	cacheMock.ExpireFunc = cacheSvc.Expire
	cacheMock.MGetFunc = cacheSvc.MGet
	cacheMock.MSetFunc = cacheSvc.MSet
	cacheMock.SetNXFunc = cacheSvc.SetNX
	cacheMock.CompareAndDeleteFunc = cacheSvc.CompareAndDelete
	cacheMock.CompareAndExpireFunc = cacheSvc.CompareAndExpire
	cacheMock.GetStreamsFunc = cacheSvc.GetStreams
	cacheMock.SetStreamsFunc = cacheSvc.SetStreams
	cacheMock.InitializeMemberDatabaseFunc = cacheSvc.InitializeMemberDatabase
	cacheMock.GetMemberChannelIDFunc = cacheSvc.GetMemberChannelID
	cacheMock.GetAllMembersFunc = cacheSvc.GetAllMembers
	cacheMock.GetMemberChannelIDWithOrgFunc = cacheSvc.GetMemberChannelIDWithOrg
	cacheMock.GetMemberChannelIDsFunc = cacheSvc.GetMemberChannelIDs
	cacheMock.AddMemberFunc = cacheSvc.AddMember
	cacheMock.CloseFunc = cacheSvc.Close
	cacheMock.IsConnectedFunc = cacheSvc.IsConnected
	cacheMock.WaitUntilReadyFunc = cacheSvc.WaitUntilReady
	cacheMock.SRemFunc = cacheSvc.SRem
	cacheMock.HSetFunc = cacheSvc.HSet

	if sadd != nil {
		cacheMock.SAddFunc = func(innerCtx context.Context, key string, members []string) (int64, error) {
			return sadd(cacheSvc, innerCtx, key, members)
		}
	} else {
		cacheMock.SAddFunc = cacheSvc.SAdd
	}

	return cacheMock, cacheSvc
}

func assertRebuildLoadedMetric(t *testing.T, operation, resource string, want float64) {
	t.Helper()

	assert.InDelta(t, want, gaugeValueForLabels(t, alarmCacheRebuildLoadedMetricName, map[string]string{
		"operation": operation,
		"resource":  resource,
	}), 0.000001)
}

func TestAddAlarm_PersistFailureDoesNotPolluteCache(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)

	as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}
	as.alarmWriter = &stubAlarmWriter{
		addFn: func(context.Context, *domain.Alarm) error {
			return errors.New("db down")
		},
	}

	ctx := t.Context()
	added, err := as.AddAlarm(ctx, domain.AddAlarmRequest{
		RoomID:     "room-1",
		UserID:     "user-1",
		ChannelID:  "ch-1",
		MemberName: "Miko",
		RoomName:   "메인방",
		UserName:   "관리자",
	})
	require.Error(t, err)
	assert.False(t, added)

	roomAlarms, err := as.GetRoomAlarms(ctx, "room-1")
	require.NoError(t, err)
	assert.Empty(t, roomAlarms)

	registry, err := as.cache.SMembers(ctx, AlarmRegistryKey)
	require.NoError(t, err)
	assert.Empty(t, registry)

	memberName, err := as.cache.HGet(ctx, MemberNameKey, "ch-1")
	require.NoError(t, err)
	assert.Empty(t, memberName)
}

func TestAddAlarm_PersistFailureLogKeepsErrorKey(t *testing.T) {
	t.Parallel()

	var logBuffer bytes.Buffer
	as := newTestAlarmService(t)
	as.logger = slog.New(slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelError}))

	as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}
	as.alarmWriter = &stubAlarmWriter{
		addFn: func(context.Context, *domain.Alarm) error {
			return errors.New("db down")
		},
	}

	added, err := as.AddAlarm(t.Context(), domain.AddAlarmRequest{
		RoomID:     "room-1",
		UserID:     "user-1",
		ChannelID:  "ch-1",
		MemberName: "Miko",
		RoomName:   "메인방",
		UserName:   "관리자",
	})
	require.Error(t, err)
	assert.False(t, added)

	var logRecord map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(logBuffer.Bytes()), &logRecord))
	assert.Equal(t, "persist alarm before cache write.failed", logRecord["event"])
	assert.Contains(t, logRecord, "error")
	assert.Equal(t, "persist alarm: stub add: db down", logRecord["error"])
	assert.Equal(t, "wrapError", logRecord["error_type"])
	assert.Equal(t, "persist alarm: stub add: db down", logRecord["error_message"])
}

func TestRemoveAlarm_PersistFailureDoesNotDeleteCache(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)

	as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}

	ctx := t.Context()
	_, err := as.AddAlarm(ctx, domain.AddAlarmRequest{
		RoomID:     "room-1",
		ChannelID:  "ch-1",
		MemberName: "Miko",
	})
	require.NoError(t, err)

	as.alarmWriter = &stubAlarmWriter{
		removeFn: func(context.Context, string, string) error {
			return errors.New("db down")
		},
	}

	removed, err := as.RemoveAlarm(ctx, "room-1", "ch-1", nil)
	require.Error(t, err)
	assert.False(t, removed)

	roomAlarms, err := as.GetRoomAlarms(ctx, "room-1")
	require.NoError(t, err)
	assert.Equal(t, []string{"ch-1"}, roomAlarms)
}

func TestClearRoomAlarms_UsesRepositoryAsAuthorityWhenConfigured(t *testing.T) {
	as := newTestAlarmService(t)

	as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}
	as.alarmRepo = &sharedalarm.Repository{}
	as.alarmWriter = &stubAlarmWriter{
		clearByRoomFn: func(context.Context, string) (int64, error) {
			return 2, nil
		},
	}

	originalFindRoomAlarms := findRoomAlarmsFromRepository

	findRoomAlarmsFromRepository = func(context.Context, *sharedalarm.Repository, string) ([]*domain.Alarm, error) {
		return []*domain.Alarm{
			{RoomID: "room-1", ChannelID: "ch-1", AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive}},
			{RoomID: "room-1", ChannelID: "ch-2", AlarmTypes: domain.AlarmTypes{domain.AlarmTypeShorts}},
		}, nil
	}

	t.Cleanup(func() {
		findRoomAlarmsFromRepository = originalFindRoomAlarms
	})

	count, err := as.ClearRoomAlarms(t.Context(), "room-1")
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestAddAlarm_PartialCacheFailure_RebuildsFromRepository(t *testing.T) {
	ctx := t.Context()
	before := counterValueForLabels(t, alarmCacheRebuildMetricName, map[string]string{
		"operation": "add",
		"result":    "ok",
	})
	beforeDurationCount := histogramCountForLabels(t, alarmCacheRebuildDurationMetricName, map[string]string{
		"operation": "add",
		"result":    "ok",
	})
	cacheMock, _ := newLenientAlarmCacheMock(ctx, t, func(cacheSvc *cache.Service, ctx context.Context, key string, members []string) (int64, error) {
		if key == AlarmRegistryKey {
			return 0, errors.New("registry add failed")
		}

		return cacheSvc.SAdd(ctx, key, members)
	})

	as := &AlarmService{
		cache:       cacheMock,
		logger:      newDiscardAlarmLogger(),
		memberData:  &mockMemberDataProvider{members: []*domain.Member{}},
		alarmRepo:   &sharedalarm.Repository{},
		alarmWriter: &stubAlarmWriter{},
	}
	cacheMock.GetClientFunc = func() valkey.Client { return nil }

	originalRebuild := rebuildSubscriberCacheFromRepository
	originalFindRoomAlarms := findRoomAlarmsFromRepository
	rebuildCalled := false

	findRoomAlarmsFromRepository = func(context.Context, *sharedalarm.Repository, string) ([]*domain.Alarm, error) {
		return nil, nil
	}

	rebuildSubscriberCacheFromRepository = func(context.Context, cache.Client, *sharedalarm.Repository) (sharedalarm.CacheWarmSummary, error) {
		rebuildCalled = true

		return sharedalarm.CacheWarmSummary{
			AlarmCount:   3,
			RoomCount:    2,
			ChannelCount: 1,
		}, nil
	}

	t.Cleanup(func() {
		rebuildSubscriberCacheFromRepository = originalRebuild
		findRoomAlarmsFromRepository = originalFindRoomAlarms
	})

	added, err := as.AddAlarm(ctx, domain.AddAlarmRequest{
		RoomID:     "room-1",
		UserID:     "user-1",
		ChannelID:  "ch-1",
		MemberName: "Miko",
	})
	require.Error(t, err)
	assert.False(t, added)
	assert.True(t, rebuildCalled)
	assert.InDelta(t, before+1, counterValueForLabels(t, alarmCacheRebuildMetricName, map[string]string{
		"operation": "add",
		"result":    "ok",
	}), 0.000001)
	assert.Equal(t, beforeDurationCount+1, histogramCountForLabels(t, alarmCacheRebuildDurationMetricName, map[string]string{
		"operation": "add",
		"result":    "ok",
	}))
	assertRebuildLoadedMetric(t, "add", "alarms", 3.0)
	assertRebuildLoadedMetric(t, "add", "rooms", 2.0)
	assertRebuildLoadedMetric(t, "add", "channels", 1.0)
}
