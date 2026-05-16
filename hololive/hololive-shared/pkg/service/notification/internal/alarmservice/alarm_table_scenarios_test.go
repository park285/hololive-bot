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
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type alarmCacheScenario struct {
	name   string
	seed   func(t *testing.T, svc *AlarmService, ctx context.Context)
	run    func(t *testing.T, svc *AlarmService, ctx context.Context) (bool, error)
	assert func(t *testing.T, svc *AlarmService, ctx context.Context, changed bool)
}

func alarmAddRemoveCacheScenarios(baseReq domain.AddAlarmRequest) []alarmCacheScenario {
	return append(alarmAddScenarios(baseReq), alarmRemoveScenarios(baseReq)...)
}

func alarmAddScenarios(baseReq domain.AddAlarmRequest) []alarmCacheScenario {
	return append(alarmAddRegistryScenarios(baseReq), alarmAddDuplicateScenarios(baseReq)...)
}

func alarmAddRegistryScenarios(baseReq domain.AddAlarmRequest) []alarmCacheScenario {
	return []alarmCacheScenario{
		{
			name: "add 신규 알람은 cache/registry를 갱신한다",
			run: func(t *testing.T, svc *AlarmService, ctx context.Context) (bool, error) {
				t.Helper()

				return svc.AddAlarm(ctx, baseReq)
			},
			assert: func(t *testing.T, svc *AlarmService, ctx context.Context, changed bool) {
				t.Helper()
				assert.True(t, changed)

				roomChannels, err := svc.cache.SMembers(ctx, AlarmKeyPrefix+baseReq.RoomID)
				require.NoError(t, err)
				assert.Equal(t, []string{baseReq.ChannelID}, roomChannels)

				rooms, err := svc.cache.SMembers(ctx, AlarmRegistryKey)
				require.NoError(t, err)
				assert.Contains(t, rooms, baseReq.RoomID)

				channels, err := svc.cache.SMembers(ctx, AlarmChannelRegistryKey)
				require.NoError(t, err)
				assert.Contains(t, channels, baseReq.ChannelID)
			},
		},
		{
			name: "add 알람 타입 지정 시 타입별 subscriber 키를 정확히 갱신한다",
			run: func(t *testing.T, svc *AlarmService, ctx context.Context) (bool, error) {
				t.Helper()

				req := baseReq

				req.AlarmTypes = domain.AlarmTypes{domain.AlarmTypeCommunity}

				return svc.AddAlarm(ctx, req)
			},
			assert: func(t *testing.T, svc *AlarmService, ctx context.Context, changed bool) {
				t.Helper()
				assert.True(t, changed)

				communitySubs, err := svc.GetChannelSubscribersByType(ctx, baseReq.ChannelID, domain.AlarmTypeCommunity)
				require.NoError(t, err)
				assert.Contains(t, communitySubs, baseReq.RoomID)

				liveSubs, err := svc.GetChannelSubscribersByType(ctx, baseReq.ChannelID, domain.AlarmTypeLive)
				require.NoError(t, err)
				assert.Empty(t, liveSubs)

				shortsSubs, err := svc.GetChannelSubscribersByType(ctx, baseReq.ChannelID, domain.AlarmTypeShorts)
				require.NoError(t, err)
				assert.Empty(t, shortsSubs)
			},
		},
	}
}

func alarmAddDuplicateScenarios(baseReq domain.AddAlarmRequest) []alarmCacheScenario {
	return []alarmCacheScenario{
		{
			name: "duplicate add는 false를 반환하고 channel set 크기를 유지한다",
			seed: func(t *testing.T, svc *AlarmService, ctx context.Context) {
				t.Helper()

				added, err := svc.AddAlarm(ctx, baseReq)
				require.NoError(t, err)
				require.True(t, added)
			},
			run: func(t *testing.T, svc *AlarmService, ctx context.Context) (bool, error) {
				t.Helper()

				return svc.AddAlarm(ctx, baseReq)
			},
			assert: func(t *testing.T, svc *AlarmService, ctx context.Context, changed bool) {
				t.Helper()
				assert.False(t, changed)

				roomChannels, err := svc.cache.SMembers(ctx, AlarmKeyPrefix+baseReq.RoomID)
				require.NoError(t, err)
				assert.Equal(t, []string{baseReq.ChannelID}, roomChannels)
			},
		},
	}
}

func alarmRemoveScenarios(baseReq domain.AddAlarmRequest) []alarmCacheScenario {
	return append(alarmRemovePartialScenarios(baseReq), alarmRemoveTerminalScenarios(baseReq)...)
}

func alarmRemovePartialScenarios(baseReq domain.AddAlarmRequest) []alarmCacheScenario {
	return []alarmCacheScenario{
		{
			name: "다중 채널 구독에서 한 채널 제거 시 room registry와 나머지 채널 구독은 유지된다",
			seed: func(t *testing.T, svc *AlarmService, ctx context.Context) {
				t.Helper()

				added, err := svc.AddAlarm(ctx, baseReq)
				require.NoError(t, err)
				require.True(t, added)

				secondReq := baseReq

				secondReq.ChannelID = "UC_TEST_2"
				secondReq.MemberName = "두번째 멤버"

				added, err = svc.AddAlarm(ctx, secondReq)
				require.NoError(t, err)
				require.True(t, added)
			},
			run: func(t *testing.T, svc *AlarmService, ctx context.Context) (bool, error) {
				t.Helper()

				return svc.RemoveAlarm(ctx, baseReq.RoomID, baseReq.ChannelID, nil)
			},
			assert: func(t *testing.T, svc *AlarmService, ctx context.Context, changed bool) {
				t.Helper()
				assert.True(t, changed)

				roomChannels, err := svc.cache.SMembers(ctx, AlarmKeyPrefix+baseReq.RoomID)
				require.NoError(t, err)
				assert.ElementsMatch(t, []string{"UC_TEST_2"}, roomChannels)

				rooms, err := svc.cache.SMembers(ctx, AlarmRegistryKey)
				require.NoError(t, err)
				assert.Contains(t, rooms, baseReq.RoomID)

				channelRegistry, err := svc.cache.SMembers(ctx, AlarmChannelRegistryKey)
				require.NoError(t, err)
				assert.NotContains(t, channelRegistry, baseReq.ChannelID)
				assert.Contains(t, channelRegistry, "UC_TEST_2")
			},
		},
	}
}

func alarmRemoveTerminalScenarios(baseReq domain.AddAlarmRequest) []alarmCacheScenario {
	return []alarmCacheScenario{
		{
			name: "remove existing alarm은 room 알람과 registry를 정리한다",
			seed: func(t *testing.T, svc *AlarmService, ctx context.Context) {
				t.Helper()

				added, err := svc.AddAlarm(ctx, baseReq)
				require.NoError(t, err)
				require.True(t, added)
			},
			run: func(t *testing.T, svc *AlarmService, ctx context.Context) (bool, error) {
				t.Helper()

				return svc.RemoveAlarm(ctx, baseReq.RoomID, baseReq.ChannelID, nil)
			},
			assert: func(t *testing.T, svc *AlarmService, ctx context.Context, changed bool) {
				t.Helper()
				assert.True(t, changed)

				roomChannels, err := svc.cache.SMembers(ctx, AlarmKeyPrefix+baseReq.RoomID)
				require.NoError(t, err)
				assert.Empty(t, roomChannels)

				rooms, err := svc.cache.SMembers(ctx, AlarmRegistryKey)
				require.NoError(t, err)
				assert.NotContains(t, rooms, baseReq.RoomID)
			},
		},
		{
			name: "remove missing alarm은 false를 반환한다",
			run: func(t *testing.T, svc *AlarmService, ctx context.Context) (bool, error) {
				t.Helper()

				return svc.RemoveAlarm(ctx, baseReq.RoomID, "UC_UNKNOWN", nil)
			},
			assert: func(t *testing.T, _ *AlarmService, _ context.Context, changed bool) {
				t.Helper()
				assert.False(t, changed)
			},
		},
	}
}

func TestAlarmService_AddRemoveCacheScenarios_TableDriven(t *testing.T) {
	t.Parallel()

	baseReq := domain.AddAlarmRequest{
		RoomID:     "room-1",
		UserID:     "user-1",
		ChannelID:  "UC_TEST",
		MemberName: "테스트 멤버",
		RoomName:   "테스트 방",
		UserName:   "테스트 사용자",
	}

	for _, tc := range alarmAddRemoveCacheScenarios(baseReq) {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := newTestAlarmService(t)

			svc.memberData = &mockMemberDataProvider{members: []*domain.Member{}}

			ctx := t.Context()

			if tc.seed != nil {
				tc.seed(t, svc, ctx)
			}

			changed, err := tc.run(t, svc, ctx)
			require.NoError(t, err)
			tc.assert(t, svc, ctx, changed)
		})
	}
}

func TestAlarmPersistence_RoundTripScenarios_TableDriven(t *testing.T) {
	t.Parallel()

	type scenario struct {
		name string
		run  func(t *testing.T, svc *AlarmService, ctx context.Context)
	}

	roundTripStart := time.Date(2026, time.March, 5, 11, 25, 42, 0, time.UTC)

	scenarios := []scenario{
		{
			name: "MarkAsNotified roundtrip은 분 단위 정규화 + SentAt map을 유지한다",
			run: func(t *testing.T, svc *AlarmService, ctx context.Context) {
				t.Helper()
				require.NoError(t, svc.MarkAsNotified(ctx, "stream-roundtrip", roundTripStart, 5))
				require.NoError(t, svc.MarkAsNotified(ctx, "stream-roundtrip", roundTripStart, 3))

				var data NotifiedData
				require.NoError(t, svc.cache.Get(ctx, NotifiedKeyPrefix+"stream-roundtrip", &data))
				assert.Equal(t, normalizeScheduledMinute(roundTripStart).Format(time.RFC3339), data.StartScheduled)
				assert.True(t, data.SentAt[5])
				assert.True(t, data.SentAt[3])
			},
		},
		{
			name: "MarkAsNotified는 스케줄 변경 시 이전 SentAt 맵을 초기화한다",
			run: func(t *testing.T, svc *AlarmService, ctx context.Context) {
				t.Helper()

				firstStart := time.Date(2026, time.March, 5, 11, 25, 42, 0, time.UTC)
				secondStart := firstStart.Add(7 * time.Minute)

				require.NoError(t, svc.MarkAsNotified(ctx, "stream-reset", firstStart, 5))
				require.NoError(t, svc.MarkAsNotified(ctx, "stream-reset", secondStart, 3))

				var data NotifiedData
				require.NoError(t, svc.cache.Get(ctx, NotifiedKeyPrefix+"stream-reset", &data))
				assert.Equal(t, normalizeScheduledMinute(secondStart).Format(time.RFC3339), data.StartScheduled)
				assert.False(t, data.SentAt[5])
				assert.True(t, data.SentAt[3])
			},
		},
		{
			name: "UpcomingEvent roundtrip은 TTL 윈도우 내 true, 즉시 만료 설정 시 false",
			run: func(t *testing.T, svc *AlarmService, ctx context.Context) {
				t.Helper()

				start := time.Now().UTC().Add(10 * time.Minute).Truncate(time.Minute)
				stream := &domain.Stream{
					ID:             "stream-upcoming",
					ChannelID:      "channel-1",
					Title:          "테스트 예정 방송",
					StartScheduled: &start,
				}

				require.NoError(t, svc.MarkUpcomingEventNotified(ctx, "room-1", "channel-1", stream))
				assert.True(t, svc.WasUpcomingEventNotifiedRecently(ctx, "room-1", "channel-1", stream, time.Minute))
				assert.False(t, svc.WasUpcomingEventNotifiedRecently(ctx, "room-1", "channel-1", stream, 0))
			},
		},
	}

	for _, tc := range scenarios {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := newTestAlarmService(t)
			ctx := t.Context()
			tc.run(t, svc, ctx)
		})
	}
}
