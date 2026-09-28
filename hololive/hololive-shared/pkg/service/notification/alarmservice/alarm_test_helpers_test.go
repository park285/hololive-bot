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
	"log/slog"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/internal/service/notification/alarmcache"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
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

// mockMemberDataProvider: 테스트용 멤버 데이터 프로바이더.
type mockMemberDataProvider struct {
	members []*domain.Member
}

func (m *mockMemberDataProvider) FindMemberByChannelID(channelID string) *domain.Member {
	for _, member := range m.members {
		if member.ChannelID == channelID {
			return member
		}
	}

	return nil
}

func (m *mockMemberDataProvider) FindMemberByName(_ string) *domain.Member { return nil }

func (m *mockMemberDataProvider) FindMemberByAlias(_ string) *domain.Member { return nil }

func (m *mockMemberDataProvider) GetChannelIDs() []string { return []string{} }

func (m *mockMemberDataProvider) LoadAllMembers() ([]*domain.Member, error) { return m.members, nil }

func (m *mockMemberDataProvider) WithContext(_ context.Context) domain.MemberDataProvider { return m }

func (m *mockMemberDataProvider) FindMembersByName(_ string) []*domain.Member {
	return []*domain.Member{}
}

func (m *mockMemberDataProvider) FindMembersByAlias(_ string) []*domain.Member {
	return []*domain.Member{}
}

// newTestAlarmService는 격리된 PG(dbtest)와 miniredis를 쓰는 서비스를 만든다. PG가 구독·방 이름의 원천이다.
func newTestAlarmService(t *testing.T) *AlarmService {
	t.Helper()

	ctx := t.Context()
	cacheClient := sharedtestutil.NewTestCacheService(ctx, t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	pool := dbtest.NewPool(t)
	repository := sharedalarm.NewRepository(&databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}, logger)

	service := &AlarmService{
		cache:           cacheClient,
		alarmRepository: repository,
		alarmWriter:     repository,
		logger:          logger,
		targetPolicy:    sharedchecker.NewTargetMinutePolicyFromConfigured([]int{30, 15, 5, 1}),
	}
	memberDataFn := func() domain.MemberDataProvider { return service.memberData }

	service.cacheState = alarmcache.NewState(cacheClient, memberDataFn, logger)

	return service
}
