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
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/internal/service/notification/alarmcache"
	"github.com/kapu/hololive-shared/internal/service/notification/platformmap"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
	dedup "github.com/kapu/hololive-shared/pkg/service/alarm/dedup"
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

	testMetricLabelOperation  = "operation"
	testMetricLabelResult     = "result"
	testWarmOperation         = "warm"
	testNextStreamStatusField = "status"
	testFallbackChannelID     = "default"
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

func (m *mockMemberDataProvider) GetAllMembers() []*domain.Member { return m.members }

func (m *mockMemberDataProvider) WithContext(_ context.Context) domain.MemberDataProvider { return m }

func (m *mockMemberDataProvider) FindMembersByName(_ string) []*domain.Member {
	return []*domain.Member{}
}

func (m *mockMemberDataProvider) FindMembersByAlias(_ string) []*domain.Member {
	return []*domain.Member{}
}

func newTestAlarmService(t *testing.T) *AlarmService {
	t.Helper()

	ctx := t.Context()
	cacheClient := sharedtestutil.NewTestCacheService(ctx, t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	service := &AlarmService{
		cache:        cacheClient,
		logger:       logger,
		targetPolicy: sharedchecker.NewTargetMinutePolicyFromConfigured([]int{30, 15, 5, 1}),
	}
	memberDataFn := func() domain.MemberDataProvider { return service.memberData }

	service.cacheState = alarmcache.NewState(cacheClient, memberDataFn, logger)
	service.platformMapper = platformmap.NewMapper(cacheClient, memberDataFn, logger)

	return service
}

func requireUpcomingEventMarker(ctx context.Context, t *testing.T, as *AlarmService, roomID, channelID string, stream *domain.Stream) {
	t.Helper()

	require.NotNil(t, stream, "stream must not be nil")
	require.NotNil(t, stream.StartScheduled, "stream.StartScheduled must not be nil")

	key := as.buildUpcomingEventKey(roomID, channelID, stream.ID, stream.Title, *stream.StartScheduled)

	var data dedup.UpcomingEventNotifiedData

	require.NoError(t, as.cache.Get(ctx, key, &data))
	require.NotEmpty(t, data.NotifiedAt, "upcoming event marker missing at key %s", key)

	notifiedAt, err := time.Parse(time.RFC3339, data.NotifiedAt)
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().UTC(), notifiedAt, time.Minute)
}
