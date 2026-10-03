package subscriptions

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/subscriptions/internal/alarmcache"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/privacylog"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
)

type stubAlarmWriter struct {
	addFn         func(context.Context, *domain.Alarm) error
	removeFn      func(context.Context, string, string) error
	removeHostFn  func(context.Context, string, string, string) error
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

func (w *stubAlarmWriter) RemoveHost(ctx context.Context, roomID, channelID, hostID string) error {
	if w.removeHostFn != nil {
		if err := w.removeHostFn(ctx, roomID, channelID, hostID); err != nil {
			return fmt.Errorf("stub remove host: %w", err)
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

	cacheClient := sharedtestutil.NewTestCacheService(ctx, t)
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
	cacheMock.SetNXFunc = cacheClient.SetNX
	cacheMock.CompareAndDeleteFunc = cacheClient.CompareAndDelete
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

// stubSuccessfulRebuild는 cache 변경 실패 뒤의 repository rebuild를 성공으로 고정해, 로그에 변경 실패만 남는지 본다.
func stubSuccessfulRebuild(t *testing.T) {
	t.Helper()

	original := rebuildSubscriberCacheFromRepository

	rebuildSubscriberCacheFromRepository = func(context.Context, cache.Client, *sharedalarm.Repository) (sharedalarm.CacheWarmSummary, error) {
		return sharedalarm.CacheWarmSummary{}, nil
	}

	t.Cleanup(func() {
		rebuildSubscriberCacheFromRepository = original
	})
}

func decodeSingleJSONLog(t *testing.T, logBuffer *bytes.Buffer) map[string]any {
	t.Helper()

	var logRecord map[string]any

	require.NoError(t, jsonv2.Unmarshal(bytes.TrimSpace(logBuffer.Bytes()), &logRecord))

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
	added, err := as.AddAlarm(ctx, &domain.AddAlarmRequest{
		RoomID:     testRoomID,
		UserID:     testUserID,
		ChannelID:  testChannelID,
		MemberName: testMemberName,
		RoomName:   "메인방",
		UserName:   "관리자",
	})
	require.Error(t, err)
	assert.False(t, added)

	roomAlarms, err := as.GetRoomAlarms(ctx, testRoomID)
	require.NoError(t, err)
	assert.Empty(t, roomAlarms)

	liveSubscribers, err := as.GetChannelSubscribersByType(ctx, testChannelID, domain.AlarmTypeLive)
	require.NoError(t, err)
	assert.Empty(t, liveSubscribers)

	memberName, err := as.cache.HGet(ctx, sharedalarmkeys.MemberNameKey, testChannelID)
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

	added, err := as.AddAlarm(t.Context(), &domain.AddAlarmRequest{
		RoomID:     testRoomID,
		UserID:     testUserID,
		ChannelID:  testChannelID,
		MemberName: testMemberName,
		RoomName:   "메인방",
		UserName:   "관리자",
	})
	require.Error(t, err)
	assert.False(t, added)

	var logRecord map[string]any

	require.NoError(t, jsonv2.Unmarshal(bytes.TrimSpace(logBuffer.Bytes()), &logRecord))
	assert.Equal(t, "persist alarm before cache write.failed", logRecord["event"])
	assert.NotContains(t, logRecord, "error")
	assert.Equal(t, "wrapError", logRecord["error_type"])
	assert.Equal(t, "persist alarm: stub add: db down", logRecord["error_message"])
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

				return as.deleteAlarmBeforeCacheRemoval(ctx, testRoomID, testChannelID, "")
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
					RoomID:     testRoomID,
					ChannelID:  testChannelID,
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

	err := as.deleteRoomAlarmsBeforeCacheClear(t.Context(), testRoomID)
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
				cacheMock.DoMultiFunc = func(context.Context, ...valkey.Completed) []valkey.ValkeyResult {
					return nil
				}
			},
			wantEvent: "rebuild clear cache from repository.failed",
			wantError: "clear room alarms: clear channel subscribers pipeline: execute subscriber key removal: clear channel subscribers: unexpected SREM result count: 0",
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

	stubSuccessfulRebuild(t)

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

			err := as.clearRoomAlarmsCacheMutation(ctx, testRoomID, []string{testChannelID})
			require.Error(t, err)

			logRecord := decodeSingleJSONLog(t, &logBuffer)
			assert.Equal(t, tt.wantEvent, logRecord["event"])
			assert.NotContains(t, logRecord, "error")
			assert.Equal(t, "wrapError", logRecord["error_type"])
			assert.Equal(t, tt.wantError, logRecord["error_message"])
		})
	}
}

type alarmBackgroundWarningCase struct {
	name         string
	setup        func(*cachemocks.Client)
	run          func(context.Context, *AlarmService)
	wantEvent    string
	wantMessage  string
	wantErrorTyp string
	wantErrorMsg string
}

func alarmBackgroundWarningCases() []alarmBackgroundWarningCase {
	return []alarmBackgroundWarningCase{
		{
			name: "clear_room_cleanup_channel_registry",
			setup: func(cacheMock *cachemocks.Client) {
				cacheMock.SRemFunc = func(context.Context, string, []string) (int64, error) {
					return 0, errors.New("srem failed")
				}
			},
			run: func(ctx context.Context, as *AlarmService) {
				as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}
				as.cleanupClearedRoomAlarmChannel(ctx, testRoomID, testChannelID)
			},
			wantEvent:    "cleanup channel registry during room alarm clear.failed",
			wantMessage:  "Failed to cleanup channel registry during room alarm clear",
			wantErrorTyp: "wrapError",
			wantErrorMsg: "cleanup channel registry: remove channel registry entry: srem failed",
		},
	}
}

func TestAlarmMutationBackgroundWarningsUseStructuredErrorAttrs(t *testing.T) {
	for _, tt := range alarmBackgroundWarningCases() {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()

			var logBuffer bytes.Buffer

			cacheMock, _ := newLenientAlarmCacheMock(ctx, t, nil)

			if tt.setup != nil {
				tt.setup(cacheMock)
			}

			warnLogger := slog.New(slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelWarn}))
			as := newTestAlarmService(t)

			as.cache = cacheMock
			as.logger = warnLogger

			memberDataFn := func() domain.MemberDataProvider { return as.memberData }

			as.cacheState = alarmcache.NewState(cacheMock, memberDataFn, warnLogger)

			tt.run(ctx, as)

			logRecord := decodeSingleJSONLog(t, &logBuffer)
			assert.Equal(t, tt.wantEvent, logRecord["event"])
			assert.Equal(t, tt.wantMessage, logRecord["msg"])
			assert.NotContains(t, logRecord, "error")
			assert.Equal(t, tt.wantErrorTyp, logRecord["error_type"])
			assert.Equal(t, tt.wantErrorMsg, logRecord["error_message"])
			assert.Equal(t, privacylog.RoomIDAttr(testRoomID).Value.String(), logRecord["room_id"])
			assert.Equal(t, testChannelID, logRecord["channel_id"])
		})
	}
}

func TestRemoveAlarm_PersistFailureDoesNotDeleteCache(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)

	as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}

	ctx := t.Context()
	_, err := as.AddAlarm(ctx, &domain.AddAlarmRequest{
		RoomID:     testRoomID,
		ChannelID:  testChannelID,
		MemberName: testMemberName,
	})
	require.NoError(t, err)

	as.alarmWriter = &stubAlarmWriter{
		removeFn: func(context.Context, string, string) error {
			return errors.New("db down")
		},
	}

	removed, err := as.RemoveAlarm(ctx, testRoomID, testChannelID, nil)
	require.Error(t, err)
	assert.False(t, removed)

	roomAlarms, err := as.GetRoomAlarms(ctx, testRoomID)
	require.NoError(t, err)
	assert.Equal(t, []string{testChannelID}, roomAlarms)
}

func TestClearRoomAlarms_UsesRepositoryAsAuthority(t *testing.T) {
	as := newTestAlarmService(t)

	as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}
	as.alarmWriter = &stubAlarmWriter{
		clearByRoomFn: func(context.Context, string) (int64, error) {
			return 2, nil
		},
	}

	originalFindRoomAlarms := findRoomAlarmsFromRepository

	findRoomAlarmsFromRepository = func(context.Context, *sharedalarm.Repository, string) ([]*domain.Alarm, error) {
		return []*domain.Alarm{
			{RoomID: testRoomID, ChannelID: testChannelID, AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive}},
			{RoomID: testRoomID, ChannelID: testOtherChannelID, AlarmTypes: domain.AlarmTypes{domain.AlarmTypeShorts}},
		}, nil
	}

	t.Cleanup(func() {
		findRoomAlarmsFromRepository = originalFindRoomAlarms
	})

	count, err := as.ClearRoomAlarms(t.Context(), testRoomID)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}
