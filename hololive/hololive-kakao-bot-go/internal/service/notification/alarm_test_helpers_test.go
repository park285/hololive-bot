package notification

import (
	"context"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/logging"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
)

// mockMemberDataProvider: 테스트용 멤버 데이터 프로바이더
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

// newTestCacheService: 테스트용 miniredis 기반 캐시 서비스 생성
func newTestCacheService(t *testing.T, ctx context.Context) *cache.Service {
	t.Helper()
	return sharedtestutil.NewTestCacheService(t, ctx)
}

// newTestAlarmService: 테스트용 AlarmService 인스턴스 생성 (miniredis 기반)
func newTestAlarmService(t *testing.T) *AlarmService {
	t.Helper()
	ctx := context.Background()
	cacheSvc := newTestCacheService(t, ctx)
	logger := logging.NewTestLogger()
	return &AlarmService{
		cache:         cacheSvc,
		logger:        logger,
		targetMinutes: []int{30, 15, 5, 1},
	}
}
