package notification

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"testing"

	"github.com/alicebob/miniredis/v2"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
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

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to start miniredis: %v", err)
	}
	t.Cleanup(func() { mr.Close() })

	port, err := strconv.Atoi(mr.Port())
	if err != nil {
		t.Fatalf("Failed to parse miniredis port: %v", err)
	}

	cfg := cache.Config{
		Host:         mr.Host(),
		Port:         port,
		Password:     "",
		DB:           0,
		DisableCache: true,
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	svc, err := cache.NewCacheService(ctx, cfg, logger)
	if err != nil {
		t.Fatalf("Failed to create test cache service: %v", err)
	}

	return svc
}

// newTestAlarmService: 테스트용 AlarmService 인스턴스 생성 (miniredis 기반)
func newTestAlarmService(t *testing.T) *AlarmService {
	t.Helper()
	ctx := context.Background()
	cacheSvc := newTestCacheService(t, ctx)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return &AlarmService{
		cache:         cacheSvc,
		logger:        logger,
		targetMinutes: []int{30, 15, 5, 1},
	}
}
