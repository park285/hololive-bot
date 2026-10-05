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

package checking

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dedup"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	alarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func TestRoomNotificationsWithScheduleChanges(t *testing.T) {
	t.Parallel()

	previous := time.Date(2026, time.May, 24, 10, 0, 0, 0, time.UTC)
	current := previous.Add(5 * time.Minute)
	stream := &domain.Stream{
		ID:             "stream-schedule-change",
		ChannelID:      testChannelID1,
		StartScheduled: &current,
		Channel:        &domain.Channel{ID: testChannelID1, Name: "Channel One"},
	}
	change := &dedup.ScheduleChange{
		PreviousScheduled: previous,
		CurrentScheduled:  current,
		Message:           "일정이 늦춰졌습니다.",
	}

	t.Run("schedule change only filters rooms without changes", func(t *testing.T) {
		t.Parallel()

		notifications := RoomNotificationsWithScheduleChanges(
			[]string{testRoomID1, testRoomID2, ""},
			stream.Channel,
			stream,
			8,
			map[string]*dedup.ScheduleChange{testRoomID1: change},
			true,
		)
		require.Len(t, notifications, 1)
		assert.Equal(t, testRoomID1, notifications[0].RoomID)
		assert.Equal(t, "일정이 늦춰졌습니다.", notifications[0].ScheduleChangeMessage)
		assert.Equal(t, alarmkeys.FormatScheduled(previous), notifications[0].ScheduleChangePreviousStart)
	})

	t.Run("regular reminder keeps rooms without schedule changes", func(t *testing.T) {
		t.Parallel()

		notifications := RoomNotificationsWithScheduleChanges(
			[]string{testRoomID1, testRoomID2},
			stream.Channel,
			stream,
			5,
			map[string]*dedup.ScheduleChange{testRoomID1: change},
			false,
		)
		require.Len(t, notifications, 2)
		assert.Equal(t, "일정이 늦춰졌습니다.", notifications[0].ScheduleChangeMessage)
		assert.Empty(t, notifications[1].ScheduleChangeMessage)
	})

	t.Run("nil stream and empty rooms return nil", func(t *testing.T) {
		t.Parallel()

		assert.Nil(t, RoomNotificationsWithScheduleChanges(nil, stream.Channel, stream, 5, nil, false))
		assert.Nil(t, RoomNotificationsWithScheduleChanges([]string{testRoomID1}, stream.Channel, nil, 5, nil, false))
	})
}

func TestScheduleChangeNotificationHelpers(t *testing.T) {
	t.Parallel()

	previous := time.Date(2026, time.May, 24, 10, 0, 0, 0, time.UTC)
	change := &dedup.ScheduleChange{
		PreviousScheduled: previous,
		Message:           "일정이 앞당겨졌습니다.",
	}

	assert.True(t, ShouldSendScheduleChangeNotification(nil, false))
	assert.False(t, ShouldSendScheduleChangeNotification(nil, true))
	assert.True(t, ShouldSendScheduleChangeNotification(change, true))

	message, previousScheduled := ScheduleChangeNotificationDetails(change)
	assert.Equal(t, "일정이 앞당겨졌습니다.", message)
	assert.Equal(t, alarmkeys.FormatScheduled(previous), previousScheduled)

	message, previousScheduled = ScheduleChangeNotificationDetails(nil)
	assert.Empty(t, message)
	assert.Empty(t, previousScheduled)
}

func TestLoadSubscriberRoomsByChannelFallsBackToSequentialLookup(t *testing.T) {
	t.Parallel()

	var (
		mu         sync.Mutex
		calledKeys []string
	)

	cacheClient := &cachemocks.Client{
		GetClientFunc: func() valkey.Client {
			return nil
		},
		SMembersFunc: func(_ context.Context, key string) ([]string, error) {
			mu.Lock()

			calledKeys = append(calledKeys, key)
			mu.Unlock()

			switch key {
			case alarmkeys.ChannelSubscribersKeyPrefix + testChannelID1:
				return []string{testRoomID1, testRoomID2}, nil
			case alarmkeys.ChannelSubscribersKeyPrefix + testChannelID2:
				return []string{testRoomID3}, nil
			default:
				return nil, nil
			}
		},
	}

	got, err := LoadSubscriberRoomsByChannel(t.Context(), cacheClient, sharedalarm.NewSubscriberResolver(cacheClient, nil), []string{testChannelID1, testChannelID2, testChannelID1})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{testRoomID1, testRoomID2}, got[testChannelID1])
	assert.ElementsMatch(t, []string{testRoomID3}, got[testChannelID2])

	mu.Lock()
	defer mu.Unlock()

	assert.ElementsMatch(t, []string{
		alarmkeys.ChannelSubscribersKeyPrefix + testChannelID1,
		alarmkeys.ChannelSubscribersKeyPrefix + testChannelID2,
	}, calledKeys)
}
