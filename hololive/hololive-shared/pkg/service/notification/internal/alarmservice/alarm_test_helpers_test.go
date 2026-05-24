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

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/notification/internal/alarmcache"
	"github.com/kapu/hololive-shared/pkg/service/notification/internal/platformmap"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
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

// newTestCacheService: 테스트용 miniredis 기반 캐시 서비스 생성.
func newTestCacheService(ctx context.Context, t *testing.T) *cache.Service {
	t.Helper()

	return sharedtestutil.NewTestCacheService(t, ctx)
}

// newTestAlarmService: 테스트용 AlarmService 인스턴스 생성 (miniredis 기반).
func newTestAlarmService(t *testing.T) *AlarmService {
	t.Helper()

	ctx := t.Context()
	cacheClient := newTestCacheService(ctx, t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	return &AlarmService{
		cache:          cacheClient,
		logger:         logger,
		targetPolicy:   sharedchecker.NewTargetMinutePolicyFromConfigured([]int{30, 15, 5, 1}),
		cacheState:     alarmcache.NewState(cacheClient, nil, logger),
		platformMapper: platformmap.NewMapper(cacheClient, nil, logger),
	}
}
