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

package subscriptions

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
)

type alarmCacheScenario struct {
	name   string
	seed   func(t *testing.T, service *AlarmService, ctx context.Context)
	run    func(t *testing.T, service *AlarmService, ctx context.Context) (bool, error)
	assert func(t *testing.T, service *AlarmService, ctx context.Context, changed bool)
}

func alarmAddRemoveCacheScenarios(baseReq *domain.AddAlarmRequest) []alarmCacheScenario {
	return append(alarmAddScenarios(baseReq), alarmRemoveScenarios(baseReq)...)
}

func alarmAddScenarios(baseReq *domain.AddAlarmRequest) []alarmCacheScenario {
	return append(alarmAddRegistryScenarios(baseReq), alarmAddDuplicateScenarios(baseReq)...)
}

func alarmAddRegistryScenarios(baseReq *domain.AddAlarmRequest) []alarmCacheScenario {
	return []alarmCacheScenario{
		{
			name: "add 신규 알람은 PG 구독과 channel registry를 갱신한다",
			run: func(t *testing.T, service *AlarmService, ctx context.Context) (bool, error) {
				t.Helper()

				return service.AddAlarm(ctx, baseReq)
			},
			assert: func(t *testing.T, service *AlarmService, ctx context.Context, changed bool) {
				t.Helper()
				assert.True(t, changed)

				roomChannels, err := service.GetRoomAlarms(ctx, baseReq.RoomID)
				require.NoError(t, err)
				assert.Equal(t, []string{baseReq.ChannelID}, roomChannels)

				channels, err := service.cache.SMembers(ctx, sharedalarmkeys.AlarmChannelRegistryKey)
				require.NoError(t, err)
				assert.Contains(t, channels, baseReq.ChannelID)
			},
		},
		{
			name: "add 알람 타입 지정 시 타입별 subscriber 키를 정확히 갱신한다",
			run: func(t *testing.T, service *AlarmService, ctx context.Context) (bool, error) {
				t.Helper()

				req := *baseReq

				req.AlarmTypes = domain.AlarmTypes{domain.AlarmTypeCommunity}

				return service.AddAlarm(ctx, &req)
			},
			assert: func(t *testing.T, service *AlarmService, ctx context.Context, changed bool) {
				t.Helper()
				assert.True(t, changed)

				communitySubs, err := service.GetChannelSubscribersByType(ctx, baseReq.ChannelID, domain.AlarmTypeCommunity)
				require.NoError(t, err)
				assert.Contains(t, communitySubs, baseReq.RoomID)

				liveSubs, err := service.GetChannelSubscribersByType(ctx, baseReq.ChannelID, domain.AlarmTypeLive)
				require.NoError(t, err)
				assert.Empty(t, liveSubs)

				shortsSubs, err := service.GetChannelSubscribersByType(ctx, baseReq.ChannelID, domain.AlarmTypeShorts)
				require.NoError(t, err)
				assert.Empty(t, shortsSubs)
			},
		},
	}
}

func alarmAddDuplicateScenarios(baseReq *domain.AddAlarmRequest) []alarmCacheScenario {
	return []alarmCacheScenario{
		{
			name: "duplicate add는 false를 반환하고 channel set 크기를 유지한다",
			seed: func(t *testing.T, service *AlarmService, ctx context.Context) {
				t.Helper()

				added, err := service.AddAlarm(ctx, baseReq)
				require.NoError(t, err)
				require.True(t, added)
			},
			run: func(t *testing.T, service *AlarmService, ctx context.Context) (bool, error) {
				t.Helper()

				return service.AddAlarm(ctx, baseReq)
			},
			assert: func(t *testing.T, service *AlarmService, ctx context.Context, changed bool) {
				t.Helper()
				assert.False(t, changed)

				roomChannels, err := service.GetRoomAlarms(ctx, baseReq.RoomID)
				require.NoError(t, err)
				assert.Equal(t, []string{baseReq.ChannelID}, roomChannels)
			},
		},
	}
}

func alarmRemoveScenarios(baseReq *domain.AddAlarmRequest) []alarmCacheScenario {
	return append(alarmRemovePartialScenarios(baseReq), alarmRemoveTerminalScenarios(baseReq)...)
}

func alarmRemovePartialScenarios(baseReq *domain.AddAlarmRequest) []alarmCacheScenario {
	return []alarmCacheScenario{
		{
			name: "다중 채널 구독에서 한 채널 제거 시 나머지 채널 구독은 유지된다",
			seed: func(t *testing.T, service *AlarmService, ctx context.Context) {
				t.Helper()

				added, err := service.AddAlarm(ctx, baseReq)
				require.NoError(t, err)
				require.True(t, added)

				secondReq := *baseReq

				secondReq.ChannelID = "UC_TEST_2"

				added, err = service.AddAlarm(ctx, &secondReq)
				require.NoError(t, err)
				require.True(t, added)
			},
			run: func(t *testing.T, service *AlarmService, ctx context.Context) (bool, error) {
				t.Helper()

				return service.RemoveAlarm(ctx, baseReq.RoomID, baseReq.ChannelID, nil)
			},
			assert: func(t *testing.T, service *AlarmService, ctx context.Context, changed bool) {
				t.Helper()
				assert.True(t, changed)

				roomChannels, err := service.GetRoomAlarms(ctx, baseReq.RoomID)
				require.NoError(t, err)
				assert.ElementsMatch(t, []string{"UC_TEST_2"}, roomChannels)

				channelRegistry, err := service.cache.SMembers(ctx, sharedalarmkeys.AlarmChannelRegistryKey)
				require.NoError(t, err)
				assert.NotContains(t, channelRegistry, baseReq.ChannelID)
				assert.Contains(t, channelRegistry, "UC_TEST_2")
			},
		},
	}
}

func alarmRemoveTerminalScenarios(baseReq *domain.AddAlarmRequest) []alarmCacheScenario {
	return []alarmCacheScenario{
		{
			name: "remove existing alarm은 PG 구독과 subscriber cache를 정리한다",
			seed: func(t *testing.T, service *AlarmService, ctx context.Context) {
				t.Helper()

				added, err := service.AddAlarm(ctx, baseReq)
				require.NoError(t, err)
				require.True(t, added)
			},
			run: func(t *testing.T, service *AlarmService, ctx context.Context) (bool, error) {
				t.Helper()

				return service.RemoveAlarm(ctx, baseReq.RoomID, baseReq.ChannelID, nil)
			},
			assert: func(t *testing.T, service *AlarmService, ctx context.Context, changed bool) {
				t.Helper()
				assert.True(t, changed)

				roomChannels, err := service.GetRoomAlarms(ctx, baseReq.RoomID)
				require.NoError(t, err)
				assert.Empty(t, roomChannels)

				liveSubs, err := service.GetChannelSubscribersByType(ctx, baseReq.ChannelID, domain.AlarmTypeLive)
				require.NoError(t, err)
				assert.Empty(t, liveSubs)
			},
		},
		{
			name: "remove missing alarm은 false를 반환한다",
			run: func(t *testing.T, service *AlarmService, ctx context.Context) (bool, error) {
				t.Helper()

				return service.RemoveAlarm(ctx, baseReq.RoomID, "UC_UNKNOWN", nil)
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
		RoomID:    testRoomID,
		UserID:    testUserID,
		ChannelID: testUCChannelID,
		RoomName:  "테스트 방",
		UserName:  "테스트 사용자",
	}

	for _, tc := range alarmAddRemoveCacheScenarios(&baseReq) {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			service := newTestAlarmService(t)

			service.memberData = &mockMemberDataProvider{members: []*domain.Member{}}

			ctx := t.Context()

			if tc.seed != nil {
				tc.seed(t, service, ctx)
			}

			changed, err := tc.run(t, service, ctx)
			require.NoError(t, err)
			tc.assert(t, service, ctx, changed)
		})
	}
}
