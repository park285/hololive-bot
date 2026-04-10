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

package notification

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newDiscardAlarmLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestAlarmKeyHelpers(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	assert.Equal(t, AlarmKeyPrefix+"room-1", as.getAlarmKey("room-1"))
	assert.Equal(t, "room-1", as.getRegistryKey("room-1"))
	assert.Equal(t, ChannelSubscribersKeyPrefix+"ch-1", as.channelSubscribersKey("ch-1"))
}

func TestAlarmCacheNameAndSubscriberHelpers(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := t.Context()

	require.NoError(t, as.CacheMemberName(ctx, "ch-1", "Miko"))

	name, err := as.GetMemberName(ctx, "ch-1")
	require.NoError(t, err)
	assert.Equal(t, "Miko", name)

	require.NoError(t, as.SetRoomName(ctx, "room-1", "메인방"))
	require.NoError(t, as.SetUserName(ctx, "user-1", "관리자"))

	roomName, err := as.cache.HGet(ctx, RoomNamesCacheKey, "room-1")
	require.NoError(t, err)
	assert.Equal(t, "메인방", roomName)

	userName, err := as.cache.HGet(ctx, UserNamesCacheKey, "user-1")
	require.NoError(t, err)
	assert.Equal(t, "관리자", userName)

	_, err = as.cache.SAdd(ctx, as.channelSubscribersKeyByType("ch-1", domain.AlarmTypeLive), []string{"room-1"})
	require.NoError(t, err)

	_, err = as.cache.SAdd(ctx, as.channelSubscribersKeyByType("ch-1", domain.AlarmTypeCommunity), []string{"room-1"})
	require.NoError(t, err)

	_, err = as.cache.SAdd(ctx, as.channelSubscribersKeyByType("ch-1", domain.AlarmTypeShorts), []string{"room-1"})
	require.NoError(t, err)

	liveSubs, err := as.GetChannelSubscribersByType(ctx, "ch-1", domain.AlarmTypeLive)
	require.NoError(t, err)
	assert.Equal(t, []string{"room-1"}, liveSubs)

	communitySubs, err := as.GetChannelSubscribersByType(ctx, "ch-1", domain.AlarmTypeCommunity)
	require.NoError(t, err)
	assert.Equal(t, []string{"room-1"}, communitySubs)

	shortsSubs, err := as.GetChannelSubscribersByType(ctx, "ch-1", domain.AlarmTypeShorts)
	require.NoError(t, err)
	assert.Equal(t, []string{"room-1"}, shortsSubs)
}

func TestGetDistinctRoomsAndAllAlarmKeys(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := t.Context()

	_, err := as.cache.SAdd(ctx, AlarmRegistryKey, []string{"room-1", "room-2", ""})
	require.NoError(t, err)

	_, err = as.cache.SAdd(ctx, as.getAlarmKey("room-1"), []string{"ch-1", "ch-2"})
	require.NoError(t, err)

	_, err = as.cache.SAdd(ctx, as.getAlarmKey("room-2"), []string{"ch-3"})
	require.NoError(t, err)

	require.NoError(t, as.CacheMemberName(ctx, "ch-1", "Miko"))
	require.NoError(t, as.CacheMemberName(ctx, "ch-2", "Suisei"))
	require.NoError(t, as.CacheMemberName(ctx, "ch-3", "Aqua"))
	require.NoError(t, as.SetRoomName(ctx, "room-1", "메인방"))

	rooms, err := as.GetDistinctRooms(ctx)
	require.NoError(t, err)
	sort.Strings(rooms)
	assert.Equal(t, []string{"room-1", "room-2"}, rooms)

	alarms, err := as.GetAllAlarmKeys(ctx)
	require.NoError(t, err)
	require.Len(t, alarms, 3)

	byRoom := map[string][]*domain.AlarmEntry{}

	for _, entry := range alarms {
		byRoom[entry.RoomID] = append(byRoom[entry.RoomID], entry)
	}

	require.Len(t, byRoom["room-1"], 2)

	for _, entry := range byRoom["room-1"] {
		assert.Equal(t, "메인방", entry.RoomName)
	}

	require.Len(t, byRoom["room-2"], 1)
	assert.Equal(t, "room-2", byRoom["room-2"][0].RoomName)
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

	var data NotifiedData
	require.NoError(t, as.cache.Get(ctx, NotifiedKeyPrefix+"stream-1", &data))
	require.Equal(t, normalizeScheduledMinute(start).Format(time.RFC3339), data.StartScheduled)
	assert.True(t, data.SentAt[5])
	assert.True(t, data.SentAt[3])

	moved := start.Add(2 * time.Minute)
	require.NoError(t, as.MarkAsNotified(ctx, "stream-1", moved, 1))
	require.NoError(t, as.cache.Get(ctx, NotifiedKeyPrefix+"stream-1", &data))
	require.Equal(t, normalizeScheduledMinute(moved).Format(time.RFC3339), data.StartScheduled)
	assert.True(t, data.SentAt[1])
}

func TestMarkAsNotified_SetFailure(t *testing.T) {
	t.Parallel()

	as := &AlarmService{
		cache: &cachemocks.Client{
			GetFunc: func(context.Context, string, any) error {
				return nil
			},
			SetFunc: func(context.Context, string, any, time.Duration) error {
				return errors.New("set failed")
			},
		},
		logger: newDiscardAlarmLogger(),
	}

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
	assert.Equal(t, []int{3, 1}, updated)
	assert.Equal(t, []int{3, 1}, as.GetTargetMinutes())

	var nilService *AlarmService
	require.NoError(t, nilService.Close(context.Background()))
	require.NoError(t, as.Close(t.Context()))
}
