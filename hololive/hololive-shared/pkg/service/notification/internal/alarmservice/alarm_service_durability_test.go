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
	"github.com/kapu/hololive-shared/pkg/service/notification/internal/alarmcache"
	"github.com/kapu/hololive-shared/pkg/service/notification/internal/platformmap"
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

	cacheClient := newTestCacheService(ctx, t)
	cacheMock := cachemocks.NewLenientClient()

	cacheMock.BuilderFunc = cacheClient.Builder
	cacheMock.BFunc = cacheClient.B
	cacheMock.GetClientFunc = cacheClient.GetClient
	cacheMock.DoMultiFunc = cacheClient.DoMulti
	cacheMock.SMembersFunc = cacheClient.SMembers
	cacheMock.SIsMemberFunc = cacheClient.SIsMember
	cacheMock.HGetFunc = cacheClient.HGet
	cacheMock.HGetAllFunc = cacheClient.HGetAll
	cacheMock.SetFunc = cacheClient.Set
	cacheMock.GetFunc = cacheClient.Get
	cacheMock.DelFunc = cacheClient.Del
	cacheMock.DelManyFunc = cacheClient.DelMany
	cacheMock.ScanKeysFunc = cacheClient.ScanKeys
	cacheMock.HDelFunc = cacheClient.HDel
	cacheMock.HMSetFunc = cacheClient.HMSet
	cacheMock.ExistsFunc = cacheClient.Exists
	cacheMock.ExpireFunc = cacheClient.Expire
	cacheMock.MGetFunc = cacheClient.MGet
	cacheMock.MSetFunc = cacheClient.MSet
	cacheMock.SetNXFunc = cacheClient.SetNX
	cacheMock.CompareAndDeleteFunc = cacheClient.CompareAndDelete
	cacheMock.CompareAndExpireFunc = cacheClient.CompareAndExpire
	cacheMock.GetStreamsFunc = cacheClient.GetStreams
	cacheMock.SetStreamsFunc = cacheClient.SetStreams
	cacheMock.InitializeMemberDatabaseFunc = cacheClient.InitializeMemberDatabase
	cacheMock.GetMemberChannelIDFunc = cacheClient.GetMemberChannelID
	cacheMock.GetAllMembersFunc = cacheClient.GetAllMembers
	cacheMock.GetMemberChannelIDWithOrgFunc = cacheClient.GetMemberChannelIDWithOrg
	cacheMock.GetMemberChannelIDsFunc = cacheClient.GetMemberChannelIDs
	cacheMock.AddMemberFunc = cacheClient.AddMember
	cacheMock.CloseFunc = cacheClient.Close
	cacheMock.IsConnectedFunc = cacheClient.IsConnected
	cacheMock.WaitUntilReadyFunc = cacheClient.WaitUntilReady
	cacheMock.SRemFunc = cacheClient.SRem
	cacheMock.HSetFunc = cacheClient.HSet

	if sadd != nil {
		cacheMock.SAddFunc = func(innerCtx context.Context, key string, members []string) (int64, error) {
			return sadd(cacheClient, innerCtx, key, members)
		}
	} else {
		cacheMock.SAddFunc = cacheClient.SAdd
	}

	return cacheMock, cacheClient
}

func assertRebuildLoadedMetric(t *testing.T, operation, resource string, want float64) {
	t.Helper()

	assert.InDelta(t, want, gaugeValueForLabels(t, alarmCacheRebuildLoadedMetricName, map[string]string{
		"operation": operation,
		"resource":  resource,
	}), 0.000001)
}

func decodeSingleJSONLog(t *testing.T, logBuffer *bytes.Buffer) map[string]any {
	t.Helper()

	var logRecord map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(logBuffer.Bytes()), &logRecord))

	return logRecord
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

func TestAddAlarm_PersistFailureLogsWrappedEvent(t *testing.T) {
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
	assert.NotContains(t, logRecord, "error")
	assert.Equal(t, "wrapError", logRecord["error_type"])
	assert.Equal(t, "persist alarm: stub add: db down", logRecord["error_message"])
}

func TestCacheAddAlarmMutationFailureLogsWrappedEvent(t *testing.T) {
	ctx := t.Context()
	var logBuffer bytes.Buffer
	cacheMock, _ := newLenientAlarmCacheMock(ctx, t, func(cacheClient *cache.Service, ctx context.Context, key string, members []string) (int64, error) {
		if key == AlarmRegistryKey {
			return 0, errors.New("registry add failed")
		}

		return cacheClient.SAdd(ctx, key, members)
	})

	logger := slog.New(slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelError}))
	as := &AlarmService{
		cache:      cacheMock,
		logger:     logger,
		memberData: &mockMemberDataProvider{members: []*domain.Member{}},
	}
	memberDataFn := func() domain.MemberDataProvider { return as.memberData }
	as.cacheState = alarmcache.NewState(cacheMock, memberDataFn, logger)
	as.platformMapper = platformmap.NewMapper(cacheMock, memberDataFn, logger)
	cacheMock.GetClientFunc = func() valkey.Client { return nil }

	_, err := as.cacheAddAlarmMutation(ctx, addAlarmMutation{
		cacheRecord: domain.Alarm{
			RoomID:     "room-1",
			ChannelID:  "ch-1",
			MemberName: "Miko",
			AlarmTypes: domain.DefaultAlarmTypes,
		},
	})
	require.Error(t, err)

	logRecord := decodeSingleJSONLog(t, &logBuffer)
	assert.Equal(t, "rebuild add cache from repository.failed", logRecord["event"])
	assert.NotContains(t, logRecord, "error")
	assert.Equal(t, "wrapError", logRecord["error_type"])
	assert.Contains(t, logRecord["error_message"], "add alarm: add room registry: registry add failed")
}

func TestRemoveAlarmPersistFailureLogsWrappedEvents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		run       func(context.Context, *AlarmService) error
		wantEvent string
		wantError string
	}{
		{
			name: "delete_alarm_before_cache_removal",
			run: func(ctx context.Context, as *AlarmService) error {
				as.alarmWriter = &stubAlarmWriter{
					removeFn: func(context.Context, string, string) error {
						return errors.New("db down")
					},
				}

				return as.deleteAlarmBeforeCacheRemoval(ctx, "room-1", "ch-1")
			},
			wantEvent: "delete alarm before cache removal.failed",
			wantError: "delete alarm: stub remove: db down",
		},
		{
			name: "update_alarm_types_before_cache_removal",
			run: func(ctx context.Context, as *AlarmService) error {
				as.alarmWriter = &stubAlarmWriter{
					addFn: func(context.Context, *domain.Alarm) error {
						return errors.New("db down")
					},
				}

				return as.updateAlarmTypesBeforeCacheRemoval(ctx, &domain.Alarm{
					RoomID:     "room-1",
					ChannelID:  "ch-1",
					AlarmTypes: domain.AlarmTypes{domain.AlarmTypeShorts},
				})
			},
			wantEvent: "persist alarm type update before cache removal.failed",
			wantError: "persist alarm type update: stub add: db down",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var logBuffer bytes.Buffer
			as := newTestAlarmService(t)
			as.logger = slog.New(slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelError}))

			err := tt.run(t.Context(), as)
			require.Error(t, err)

			logRecord := decodeSingleJSONLog(t, &logBuffer)
			assert.Equal(t, tt.wantEvent, logRecord["event"])
			assert.NotContains(t, logRecord, "error")
			assert.Equal(t, "wrapError", logRecord["error_type"])
			assert.Equal(t, tt.wantError, logRecord["error_message"])
		})
	}
}

func TestRemoveAlarmCacheMutationFailureLogsWrappedEvents(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(*cachemocks.Client, *cache.Service)
		mutation  removeAlarmMutation
		wantEvent string
		wantError string
	}{
		{
			name: "rebuild_remove_cache_from_repository",
			setup: func(cacheMock *cachemocks.Client, _ *cache.Service) {
				cacheMock.SRemFunc = func(context.Context, string, []string) (int64, error) {
					return 0, errors.New("srem failed")
				}
			},
			mutation: removeAlarmMutation{
				effectiveRemovalTypes: domain.AlarmTypes{domain.AlarmTypeLive},
				removeRoomChannel:     true,
			},
			wantEvent: "rebuild remove cache from repository.failed",
			wantError: "remove alarm: remove room alarm: srem failed",
		},
		{
			name: "mark_room_alarms_changed_in_cache",
			setup: func(cacheMock *cachemocks.Client, _ *cache.Service) {
				cacheMock.DelFunc = func(context.Context, string) error {
					return errors.New("del failed")
				}
			},
			mutation: removeAlarmMutation{
				removeRoomChannel: true,
			},
			wantEvent: "mark room alarms changed in cache.failed",
			wantError: "mark alarm cache changed: clear empty subscriber cache marker: del failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			var logBuffer bytes.Buffer
			cacheMock, cacheClient := newLenientAlarmCacheMock(ctx, t, nil)
			tt.setup(cacheMock, cacheClient)

			as := &AlarmService{
				cache:  cacheMock,
				logger: slog.New(slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelError})),
			}

			removed, err := as.removeAlarmCacheMutation(ctx, "room-1", "ch-1", tt.mutation)
			require.Error(t, err)
			assert.False(t, removed)

			logRecord := decodeSingleJSONLog(t, &logBuffer)
			assert.Equal(t, tt.wantEvent, logRecord["event"])
			assert.NotContains(t, logRecord, "error")
			assert.Equal(t, "wrapError", logRecord["error_type"])
			assert.Equal(t, tt.wantError, logRecord["error_message"])
		})
	}
}

func TestClearRoomAlarmsPersistFailureLogsWrappedEvent(t *testing.T) {
	t.Parallel()

	var logBuffer bytes.Buffer
	as := newTestAlarmService(t)
	as.logger = slog.New(slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelError}))
	as.alarmWriter = &stubAlarmWriter{
		clearByRoomFn: func(context.Context, string) (int64, error) {
			return 0, errors.New("db down")
		},
	}

	err := as.deleteRoomAlarmsBeforeCacheClear(t.Context(), "room-1")
	require.Error(t, err)

	logRecord := decodeSingleJSONLog(t, &logBuffer)
	assert.Equal(t, "delete room alarms before cache clear.failed", logRecord["event"])
	assert.NotContains(t, logRecord, "error")
	assert.Equal(t, "wrapError", logRecord["error_type"])
	assert.Equal(t, "delete room alarms: stub clear by room: db down", logRecord["error_message"])
}

func TestClearRoomAlarmsCacheMutationFailureLogsWrappedEvents(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(*cachemocks.Client, *cache.Service)
		wantEvent string
		wantError string
	}{
		{
			name: "rebuild_clear_cache_from_repository",
			setup: func(cacheMock *cachemocks.Client, _ *cache.Service) {
				cacheMock.SRemFunc = func(context.Context, string, []string) (int64, error) {
					return 0, errors.New("srem failed")
				}
			},
			wantEvent: "rebuild clear cache from repository.failed",
			wantError: "clear room alarms: remove room alarms: srem failed",
		},
		{
			name: "mark_room_alarms_changed_in_cache",
			setup: func(cacheMock *cachemocks.Client, _ *cache.Service) {
				cacheMock.DelFunc = func(context.Context, string) error {
					return errors.New("del failed")
				}
			},
			wantEvent: "mark room alarms changed in cache.failed",
			wantError: "mark alarm cache changed: clear empty subscriber cache marker: del failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			var logBuffer bytes.Buffer
			cacheMock, cacheClient := newLenientAlarmCacheMock(ctx, t, nil)
			tt.setup(cacheMock, cacheClient)

			as := &AlarmService{
				cache:  cacheMock,
				logger: slog.New(slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelError})),
			}

			removed, err := as.clearRoomAlarmsCacheMutation(ctx, "room-1", []string{"ch-1"})
			require.Error(t, err)
			assert.Zero(t, removed)

			logRecord := decodeSingleJSONLog(t, &logBuffer)
			assert.Equal(t, tt.wantEvent, logRecord["event"])
			assert.NotContains(t, logRecord, "error")
			assert.Equal(t, "wrapError", logRecord["error_type"])
			assert.Equal(t, tt.wantError, logRecord["error_message"])
		})
	}
}

func TestAlarmMutationBackgroundWarningsUseStructuredErrorAttrs(t *testing.T) {
	tests := []struct {
		name         string
		setup        func(*cachemocks.Client)
		run          func(context.Context, *AlarmService)
		wantEvent    string
		wantMessage  string
		wantErrorTyp string
		wantErrorMsg string
	}{
		{
			name: "after_add_sync_platform_mapping",
			run: func(ctx context.Context, as *AlarmService) {
				as.afterAddAlarm(ctx, domain.AddAlarmRequest{
					RoomID:    "room-1",
					ChannelID: "ch-1",
				}, domain.AlarmTypes{domain.AlarmTypeLive})
			},
			wantEvent:    "sync platform alarm mapping after add.failed",
			wantMessage:  "Failed to sync platform alarm mapping after add",
			wantErrorTyp: "errorString",
			wantErrorMsg: "member data provider not configured",
		},
		{
			name: "after_remove_sync_platform_mapping",
			run: func(ctx context.Context, as *AlarmService) {
				as.afterRemoveAlarm(ctx, "room-1", "ch-1", removeAlarmMutation{
					effectiveRemovalTypes: domain.AlarmTypes{domain.AlarmTypeLive},
				})
			},
			wantEvent:    "sync platform alarm mapping after remove.failed",
			wantMessage:  "Failed to sync platform alarm mapping after remove",
			wantErrorTyp: "errorString",
			wantErrorMsg: "member data provider not configured",
		},
		{
			name: "clear_room_cleanup_channel_registry",
			setup: func(cacheMock *cachemocks.Client) {
				cacheMock.SRemFunc = func(context.Context, string, []string) (int64, error) {
					return 0, errors.New("srem failed")
				}
			},
			run: func(ctx context.Context, as *AlarmService) {
				as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}
				as.cleanupClearedRoomAlarmChannel(ctx, "room-1", "ch-1")
			},
			wantEvent:    "cleanup channel registry during room alarm clear.failed",
			wantMessage:  "Failed to cleanup channel registry during room alarm clear",
			wantErrorTyp: "wrapError",
			wantErrorMsg: "cleanup channel registry: remove channel registry entry: srem failed",
		},
		{
			name: "clear_room_sync_platform_mapping",
			run: func(ctx context.Context, as *AlarmService) {
				as.cleanupClearedRoomAlarmChannel(ctx, "room-1", "ch-1")
			},
			wantEvent:    "sync platform alarm mapping after clear.failed",
			wantMessage:  "Failed to sync platform alarm mapping after clear",
			wantErrorTyp: "errorString",
			wantErrorMsg: "member data provider not configured",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			var logBuffer bytes.Buffer
			cacheMock, _ := newLenientAlarmCacheMock(ctx, t, nil)
			if tt.setup != nil {
				tt.setup(cacheMock)
			}

			warnLogger := slog.New(slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelWarn}))
			as := &AlarmService{
				cache:  cacheMock,
				logger: warnLogger,
			}
			memberDataFn := func() domain.MemberDataProvider { return as.memberData }
			as.cacheState = alarmcache.NewState(cacheMock, memberDataFn, warnLogger)
			as.platformMapper = platformmap.NewMapper(cacheMock, memberDataFn, warnLogger)

			tt.run(ctx, as)

			logRecord := decodeSingleJSONLog(t, &logBuffer)
			assert.Equal(t, tt.wantEvent, logRecord["event"])
			assert.Equal(t, tt.wantMessage, logRecord["msg"])
			assert.NotContains(t, logRecord, "error")
			assert.Equal(t, tt.wantErrorTyp, logRecord["error_type"])
			assert.Equal(t, tt.wantErrorMsg, logRecord["error_message"])
			assert.Equal(t, "room-1", logRecord["room_id"])
			assert.Equal(t, "ch-1", logRecord["channel_id"])
		})
	}
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
	as.alarmRepository = &sharedalarm.Repository{}
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
	cacheMock, _ := newLenientAlarmCacheMock(ctx, t, func(cacheClient *cache.Service, ctx context.Context, key string, members []string) (int64, error) {
		if key == AlarmRegistryKey {
			return 0, errors.New("registry add failed")
		}

		return cacheClient.SAdd(ctx, key, members)
	})

	discardLogger := newDiscardAlarmLogger()
	as := &AlarmService{
		cache:           cacheMock,
		logger:          discardLogger,
		memberData:      &mockMemberDataProvider{members: []*domain.Member{}},
		alarmRepository: &sharedalarm.Repository{},
		alarmWriter:     &stubAlarmWriter{},
	}
	rebuildMemberDataFn := func() domain.MemberDataProvider { return as.memberData }
	as.cacheState = alarmcache.NewState(cacheMock, rebuildMemberDataFn, discardLogger)
	as.platformMapper = platformmap.NewMapper(cacheMock, rebuildMemberDataFn, discardLogger)
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
