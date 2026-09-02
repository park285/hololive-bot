// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package alarmservice

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/internal/service/notification/alarmcache"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func newDiscardAlarmLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestAlarmKeyHelpers(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	assert.Equal(t, sharedalarmkeys.AlarmKeyPrefix+testRoomID, as.getAlarmKey(testRoomID))
	assert.Equal(t, testRoomID, as.getRegistryKey(testRoomID))
	assert.Equal(t, sharedalarmkeys.ChannelSubscribersKeyPrefix+testChannelID, as.channelSubscribersKey(testChannelID))
}

func TestAlarmCacheNameAndSubscriberHelpers(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := t.Context()

	require.NoError(t, as.CacheMemberName(ctx, testChannelID, testMemberName))

	name, err := as.GetMemberName(ctx, testChannelID)
	require.NoError(t, err)
	assert.Equal(t, testMemberName, name)

	require.NoError(t, as.SetRoomName(ctx, testRoomID, "메인방"))
	require.NoError(t, as.SetUserName(ctx, testUserID, "관리자"))

	roomName, err := as.cache.HGet(ctx, sharedalarmkeys.RoomNamesCacheKey, testRoomID)
	require.NoError(t, err)
	assert.Equal(t, "메인방", roomName)

	userName, err := as.cache.HGet(ctx, sharedalarmkeys.UserNamesCacheKey, testUserID)
	require.NoError(t, err)
	assert.Equal(t, "관리자", userName)

	_, err = as.cache.SAdd(ctx, as.channelSubscribersKeyByType(testChannelID, domain.AlarmTypeLive), []string{testRoomID})
	require.NoError(t, err)

	_, err = as.cache.SAdd(ctx, as.channelSubscribersKeyByType(testChannelID, domain.AlarmTypeCommunity), []string{testRoomID})
	require.NoError(t, err)

	_, err = as.cache.SAdd(ctx, as.channelSubscribersKeyByType(testChannelID, domain.AlarmTypeShorts), []string{testRoomID})
	require.NoError(t, err)

	liveSubs, err := as.GetChannelSubscribersByType(ctx, testChannelID, domain.AlarmTypeLive)
	require.NoError(t, err)
	assert.Equal(t, []string{testRoomID}, liveSubs)

	communitySubs, err := as.GetChannelSubscribersByType(ctx, testChannelID, domain.AlarmTypeCommunity)
	require.NoError(t, err)
	assert.Equal(t, []string{testRoomID}, communitySubs)

	shortsSubs, err := as.GetChannelSubscribersByType(ctx, testChannelID, domain.AlarmTypeShorts)
	require.NoError(t, err)
	assert.Equal(t, []string{testRoomID}, shortsSubs)
}

func TestGetDistinctRoomsAndAllAlarmKeys(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := t.Context()

	_, err := as.cache.SAdd(ctx, sharedalarmkeys.AlarmRegistryKey, []string{testRoomID, "room-2", ""})
	require.NoError(t, err)

	_, err = as.cache.SAdd(ctx, as.getAlarmKey(testRoomID), []string{testChannelID, testOtherChannelID})
	require.NoError(t, err)

	_, err = as.cache.SAdd(ctx, as.getAlarmKey("room-2"), []string{"ch-3"})
	require.NoError(t, err)

	require.NoError(t, as.CacheMemberName(ctx, testChannelID, testMemberName))
	require.NoError(t, as.CacheMemberName(ctx, testOtherChannelID, "Suisei"))
	require.NoError(t, as.CacheMemberName(ctx, "ch-3", "Aqua"))
	require.NoError(t, as.SetRoomName(ctx, testRoomID, "메인방"))

	rooms, err := as.GetDistinctRooms(ctx)
	require.NoError(t, err)
	slices.Sort(rooms)
	assert.Equal(t, []string{testRoomID, "room-2"}, rooms)

	alarms, err := as.GetAllAlarmKeys(ctx)
	require.NoError(t, err)
	require.Len(t, alarms, 3)

	byRoom := map[string][]*domain.AlarmEntry{}

	for _, entry := range alarms {
		byRoom[entry.RoomID] = append(byRoom[entry.RoomID], entry)
	}

	require.Len(t, byRoom[testRoomID], 2)

	for _, entry := range byRoom[testRoomID] {
		assert.Equal(t, "메인방", entry.RoomName)
	}

	require.Len(t, byRoom["room-2"], 1)
	assert.Equal(t, "room-2", byRoom["room-2"][0].RoomName)
}

func TestGetAllAlarmKeysBatchesRoomMembershipRoundTrips(t *testing.T) {
	t.Parallel()

	oneRoomRoundTrips, oneRoomMaxBatch := alarmAdminRoundTrips(t, 1)
	hundredRoomRoundTrips, hundredRoomMaxBatch := alarmAdminRoundTrips(t, 100)
	hundredOneRoomRoundTrips, hundredOneRoomMaxBatch := alarmAdminRoundTrips(t, 101)

	assert.Equal(t, 4, oneRoomRoundTrips)
	assert.Equal(t, oneRoomRoundTrips, hundredRoomRoundTrips)
	assert.Equal(t, 5, hundredOneRoomRoundTrips)
	assert.Equal(t, 1, oneRoomMaxBatch)
	assert.Equal(t, alarmRoomMembershipBatchSize, hundredRoomMaxBatch)
	assert.Equal(t, alarmRoomMembershipBatchSize, hundredOneRoomMaxBatch)
}

func TestGetAllAlarmKeysSkipsOnlyFailedRoomMembershipResult(t *testing.T) {
	t.Parallel()

	service := newTestAlarmService(t)
	ctx := t.Context()
	failedRoomID := "room-failed"
	healthyRoomID := "room-healthy"
	healthyChannelID := "channel-healthy"

	_, err := service.cache.SAdd(ctx, sharedalarmkeys.AlarmRegistryKey, []string{failedRoomID, healthyRoomID})
	require.NoError(t, err)
	require.NoError(t, service.cache.Set(ctx, service.getAlarmKey(failedRoomID), "wrong-type", 0))

	_, err = service.cache.SAdd(ctx, service.getAlarmKey(healthyRoomID), []string{healthyChannelID})
	require.NoError(t, err)

	alarms, err := service.GetAllAlarmKeys(ctx)
	require.NoError(t, err)
	require.Len(t, alarms, 1)
	assert.Equal(t, healthyRoomID, alarms[0].RoomID)
	assert.Equal(t, healthyChannelID, alarms[0].ChannelID)
}

func alarmAdminRoundTrips(t *testing.T, roomCount int) (int, int) {
	t.Helper()

	service := newTestAlarmService(t)
	cacheClient := service.cache
	ctx := t.Context()
	roomIDs := make([]string, roomCount)

	for index := range roomCount {
		roomID := fmt.Sprintf("room-%03d", index)
		channelID := fmt.Sprintf("channel-%03d", index)

		roomIDs[index] = roomID

		_, err := cacheClient.SAdd(ctx, service.getAlarmKey(roomID), []string{channelID})
		require.NoError(t, err)
		require.NoError(t, cacheClient.HSet(ctx, sharedalarmkeys.MemberNameKey, channelID, channelID))
	}

	_, err := cacheClient.SAdd(ctx, sharedalarmkeys.AlarmRegistryKey, roomIDs)
	require.NoError(t, err)

	roundTrips := 0
	maxDoMultiCommands := 0
	countingCache := &cachemocks.Client{
		SMembersFunc: func(ctx context.Context, key string) ([]string, error) {
			roundTrips++
			return cacheClient.SMembers(ctx, key)
		},
		HGetAllFunc: func(ctx context.Context, key string) (map[string]string, error) {
			roundTrips++
			return cacheClient.HGetAll(ctx, key)
		},
		BatchHGetFunc: func(ctx context.Context, key string, fields []string) (map[string]string, error) {
			roundTrips++
			return cacheClient.BatchHGet(ctx, key, fields)
		},
		BFunc: func() valkey.Builder {
			return cacheClient.B()
		},
		DoMultiFunc: func(ctx context.Context, commands ...valkey.Completed) []valkey.ValkeyResult {
			roundTrips++

			maxDoMultiCommands = max(maxDoMultiCommands, len(commands))

			return cacheClient.DoMulti(ctx, commands...)
		},
	}

	service.cache = countingCache
	service.cacheState = alarmcache.NewState(countingCache, func() domain.MemberDataProvider { return service.memberData }, service.logger)

	alarms, err := service.GetAllAlarmKeys(ctx)
	require.NoError(t, err)
	require.Len(t, alarms, roomCount)

	return roundTrips, maxDoMultiCommands
}

func TestAlarmAdminCacheErrorBranches(t *testing.T) {
	t.Parallel()

	cacheErr := errors.New("cache unavailable")
	as := &AlarmService{
		cache: &cachemocks.Client{
			SMembersFunc: func(context.Context, string) ([]string, error) {
				return nil, cacheErr
			},
		},
		logger: newDiscardAlarmLogger(),
	}

	_, err := as.GetAllAlarmKeys(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get alarm registry")

	_, err = as.GetDistinctRooms(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get alarm registry")
}

func TestMarkAsNotified(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := t.Context()
	start := time.Date(2026, time.March, 4, 10, 10, 30, 0, time.UTC)

	require.NoError(t, as.MarkAsNotified(ctx, "stream-1", start, 5))
	require.NoError(t, as.MarkAsNotified(ctx, "stream-1", start, 3))

	assert.True(t, as.WasNotified(ctx, "stream-1", start, 5))
	assert.True(t, as.WasNotified(ctx, "stream-1", start, 3))

	moved := start.Add(2 * time.Minute)
	require.NoError(t, as.MarkAsNotified(ctx, "stream-1", moved, 1))
	assert.True(t, as.WasNotified(ctx, "stream-1", moved, 1))
}

func TestMarkAsNotified_SetFailure(t *testing.T) {
	t.Parallel()

	mockCache := &cachemocks.Client{
		GetFunc: func(context.Context, string, any) error {
			return nil
		},
		SetFunc: func(context.Context, string, any, time.Duration) error {
			return errors.New("set failed")
		},
	}
	discardLogger := newDiscardAlarmLogger()
	as := &AlarmService{
		cache:  mockCache,
		logger: discardLogger,
	}

	as.cacheState = alarmcache.NewState(mockCache, func() domain.MemberDataProvider { return as.memberData }, discardLogger)

	err := as.MarkAsNotified(t.Context(), "stream-1", time.Now().UTC(), 5)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mark as notified")
}

func TestTargetMinutesAndCloseHelpers(t *testing.T) {
	t.Parallel()

	as := &AlarmService{logger: newDiscardAlarmLogger()}
	assert.Equal(t, []int{5, 3, 1}, as.GetTargetMinutes())

	updated := as.UpdateAlarmAdvanceMinutes(t.Context(), 10)
	assert.Equal(t, []int{10, 3, 1}, updated)
	assert.Equal(t, []int{10, 3, 1}, as.GetTargetMinutes())

	updated = as.UpdateAlarmAdvanceMinutes(t.Context(), 1)
	assert.Equal(t, []int{1}, updated)
	assert.Equal(t, []int{1}, as.GetTargetMinutes())

	var nilService *AlarmService

	require.NoError(t, nilService.Close(t.Context()))
	require.NoError(t, as.Close(t.Context()))
}
