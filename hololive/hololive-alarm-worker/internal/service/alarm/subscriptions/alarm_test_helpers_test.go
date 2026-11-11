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
	"fmt"
	"log/slog"
	"os"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/alarmstore"
	dbtest "github.com/kapu/hololive-dbtest"
	targetpolicy "github.com/kapu/hololive-shared/pkg/alarmtiming/targetpolicy"
	"github.com/kapu/hololive-shared/pkg/domain"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
)

// operation/result/warm/status는 프로덕션이 실제로 내보내는 메트릭 라벨과 캐시 필드 이름이다.
// 프로덕션 상수를 참조하면 그 값이 바뀔 때 기대값도 함께 움직여 테스트가 깨지지 않고 조용히
// 통과하므로, 테스트가 고정하려는 값은 여기에 따로 적어 둔다.
const (
	testRoomID         = "room-1"
	testAltRoomID      = "room1"
	testUserID         = "user-1"
	testChannelID      = "ch-1"
	testOtherChannelID = "ch-2"
	testMemberName     = "Miko"
	testUCChannelID    = "UC_TEST"
	testAlphaChannelID = "UC_alpha"

	testMetricLabelOperation = "operation"
	testMetricLabelResult    = "result"
	testWarmOperation        = "warm"
	testFallbackChannelID    = "default"
)

type mockMemberDataProvider struct {
	members []*domain.Member
}

func (m *mockMemberDataProvider) FindMemberByChannelID(_ context.Context, channelID string) (*domain.Member, error) {
	for _, member := range m.members {
		if member.ChannelID == channelID {
			return member, nil
		}
	}

	return nil, domain.ErrMemberNotFound
}

func (m *mockMemberDataProvider) FindMemberByName(context.Context, string) (*domain.Member, error) {
	return nil, domain.ErrMemberNotFound
}

func (m *mockMemberDataProvider) FindMemberByAlias(context.Context, string) (*domain.Member, error) {
	return nil, domain.ErrMemberNotFound
}

func (m *mockMemberDataProvider) GetChannelIDs(context.Context) ([]string, error) {
	return []string{}, nil
}

func (m *mockMemberDataProvider) LoadAllMembers(context.Context) ([]*domain.Member, error) {
	return m.members, nil
}

// newTestAlarmService는 격리된 PG(dbtest)와 miniredis를 쓰는 서비스를 만든다. PG가 구독·방 이름의 원천이다.
func newTestAlarmService(t *testing.T) *AlarmService {
	t.Helper()

	ctx := t.Context()
	cacheClient := sharedtestutil.NewTestCacheService(ctx, t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	pool := dbtest.NewPool(t)
	repository := alarmstore.NewRepository(&databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}, logger)

	return &AlarmService{
		cache:           cacheClient,
		alarmRepository: repository,
		alarmWriter:     repository,
		logger:          logger,
		targetPolicy:    targetpolicy.NewTargetMinutePolicyFromConfigured([]int{30, 15, 5, 1}),
	}
}

// roomChannelIDs는 방이 구독한 채널 ID를 PG 정본에서 중복 없이 읽는다. UNIT B 멤버 구독 행은 채널 하나로 합친다.
func roomChannelIDs(ctx context.Context, as *AlarmService, roomID string) ([]string, error) {
	alarms, err := as.GetRoomAlarmsWithTypes(ctx, roomID)
	if err != nil {
		return nil, fmt.Errorf("room channel ids: %w", err)
	}

	return uniqueAlarmChannelIDs(alarms), nil
}

// channelSubscriberRooms는 채널·알림 종류를 구독한 방 ID를 PG 정본에서 정렬·중복 제거해 읽는다.
func channelSubscriberRooms(ctx context.Context, as *AlarmService, channelID string, alarmType domain.AlarmType) ([]string, error) {
	alarms, err := as.alarmRepository.FindByChannelAndType(ctx, channelID, alarmType)
	if err != nil {
		return nil, fmt.Errorf("channel subscriber rooms: %w", err)
	}

	rooms := make([]string, 0, len(alarms))
	for _, alarm := range alarms {
		rooms = append(rooms, alarm.RoomID)
	}

	slices.Sort(rooms)

	return slices.Compact(rooms), nil
}
