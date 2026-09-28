package apiservice

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

const (
	testChannelID1    = "UC1"
	testChannelID2    = "UC2"
	testFallbackTitle = "@fallback"
	testOrgHololive   = "Hololive"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

type channelNameMembers struct {
	members   []*domain.Member
	err       error
	loadCalls *atomic.Int32
}

func (m channelNameMembers) LoadAllMembers() ([]*domain.Member, error) {
	m.loadCalls.Add(1)

	return m.members, m.err
}

func (channelNameMembers) FindMemberByChannelID(string) *domain.Member             { return nil }
func (channelNameMembers) FindMemberByName(string) *domain.Member                  { return nil }
func (channelNameMembers) FindMemberByAlias(string) *domain.Member                 { return nil }
func (channelNameMembers) FindMembersByName(string) []*domain.Member               { return nil }
func (channelNameMembers) FindMembersByAlias(string) []*domain.Member              { return nil }
func (channelNameMembers) GetChannelIDs() []string                                 { return nil }
func (m channelNameMembers) WithContext(context.Context) domain.MemberDataProvider { return m }

func newChannelNameService(t *testing.T, members []*domain.Member, loadErr error) (*serviceImpl, int32) {
	t.Helper()

	provider := channelNameMembers{members: members, err: loadErr, loadCalls: &atomic.Int32{}}

	// strict cache는 설정하지 않은 명령마다 panic한다. 통계 cache를 유지한 채로 채널 이름 초기화가
	// 퇴역한 hololive:members를 포함해 어떤 cache 명령도 보내지 않음을 함께 검증한다.
	svc, err := New(t.Context(), cachemocks.NewStrictClient(), provider, settings.DefaultYouTubeOperationalConfig(), nil, discardLogger())
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}

	ys, ok := svc.(*serviceImpl)
	if !ok {
		t.Fatalf("New() returned %T, want *serviceImpl", svc)
	}

	return ys, provider.loadCalls.Load()
}

// 공유 channel은 입력 순서와 무관하게 최소 영속 ID 멤버의 Name을 쓴다.
func TestNew_SharedChannelUsesLowestIDRepresentativeName(t *testing.T) {
	t.Parallel()

	first := &domain.Member{ID: 3, Name: "Later", ChannelID: "UC_shared", Org: testOrgHololive}
	representative := &domain.Member{ID: 1, Name: "Earliest", ChannelID: "UC_shared", Org: testOrgHololive}
	third := &domain.Member{ID: 2, Name: "Middle", ChannelID: "UC_shared", Org: testOrgHololive}

	orders := [][]*domain.Member{
		{first, representative, third},
		{third, first, representative},
		{representative, third, first},
	}
	for _, members := range orders {
		ys, _ := newChannelNameService(t, members, nil)

		if got := ys.resolveChannelTitle("UC_shared", testFallbackTitle); got != "Earliest" {
			t.Fatalf("resolveChannelTitle(UC_shared) = %q, want Earliest (order %v)", got, []int{members[0].ID, members[1].ID, members[2].ID})
		}
	}
}

// 조직이 다른 동명 멤버와 colon이 든 이름도 channel별로 그대로 보존한다.
func TestNew_ChannelNamesKeepSameNameAcrossOrgsAndColon(t *testing.T) {
	t.Parallel()

	ys, calls := newChannelNameService(t, []*domain.Member{
		{ID: 10, Name: "Sora", ChannelID: testChannelID1, Org: testOrgHololive},
		{ID: 11, Name: "Sora", ChannelID: testChannelID2, Org: "Nijisanji"},
		{ID: 12, Name: "name:with:colon", ChannelID: "UC_colon", Org: testOrgHololive},
	}, nil)

	if calls != 1 {
		t.Fatalf("LoadAllMembers() calls = %d, want 1", calls)
	}

	want := map[string]string{testChannelID1: "Sora", testChannelID2: "Sora", "UC_colon": "name:with:colon"}
	for channelID, name := range want {
		if got := ys.resolveChannelTitle(channelID, testFallbackTitle); got != name {
			t.Fatalf("resolveChannelTitle(%q) = %q, want %q", channelID, got, name)
		}
	}

	if len(ys.channelToName) != len(want) {
		t.Fatalf("channelToName = %v, want %d entries", ys.channelToName, len(want))
	}
}

// nil 멤버·빈 channel은 무시하고, 대표 Name이 비면 차순위로 바꾸지 않고 fallbackTitle을 쓴다.
func TestNew_ChannelNamesSkipInvalidMembersAndBlankRepresentative(t *testing.T) {
	t.Parallel()

	ys, _ := newChannelNameService(t, []*domain.Member{
		nil,
		{ID: 1, Name: "NoChannel", ChannelID: ""},
		{ID: 2, Name: "", ChannelID: "UC_blank"},
		{ID: 3, Name: "Runner-up", ChannelID: "UC_blank"},
	}, nil)

	if len(ys.channelToName) != 0 {
		t.Fatalf("channelToName = %v, want empty", ys.channelToName)
	}

	if got := ys.resolveChannelTitle("UC_blank", testFallbackTitle); got != testFallbackTitle {
		t.Fatalf("resolveChannelTitle(UC_blank) = %q, want fallback %q", got, testFallbackTitle)
	}
}

// 멤버 source 오류는 service 생성을 막지 않고 재시도 없이 빈 이름 맵으로 남는다.
func TestNew_MemberLoadFailureIsNonfatal(t *testing.T) {
	t.Parallel()

	ys, calls := newChannelNameService(t, []*domain.Member{{ID: 1, Name: "Ignored", ChannelID: testChannelID1}}, errors.New("database unavailable"))

	if calls != 1 {
		t.Fatalf("LoadAllMembers() calls = %d, want 1", calls)
	}

	if len(ys.channelToName) != 0 {
		t.Fatalf("channelToName = %v, want empty after load failure", ys.channelToName)
	}

	if got := ys.resolveChannelTitle(testChannelID1, testFallbackTitle); got != testFallbackTitle {
		t.Fatalf("resolveChannelTitle() = %q, want fallback %q", got, testFallbackTitle)
	}
}

func TestNew_ReturnsUsableServiceWithoutCacheOrMembers(t *testing.T) {
	t.Parallel()

	svc, err := New(t.Context(), nil, nil, settings.DefaultYouTubeOperationalConfig(), nil, discardLogger())
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}

	if svc == nil {
		t.Fatal("New() returned nil service")
	}

	got, err := svc.GetChannelStatistics(t.Context(), nil)
	if err != nil {
		t.Fatalf("GetChannelStatistics(nil) unexpected error: %v", err)
	}

	if len(got) != 0 {
		t.Fatalf("GetChannelStatistics(nil) len = %d, want 0", len(got))
	}
}

// runtime 설정의 cache 저장·scraper phase timeout이 서비스에 그대로 들어가야 한다(stack audit A3).
func TestNew_UsesInjectedYouTubeTimeouts(t *testing.T) {
	t.Parallel()

	cfg := settings.DefaultYouTubeOperationalConfig()

	cfg.CacheSaveTimeout = 2 * time.Second
	cfg.ScraperPhaseTimeout = 9 * time.Second

	svc, err := New(t.Context(), nil, nil, cfg, nil, discardLogger())
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}

	ys, ok := svc.(*serviceImpl)
	if !ok {
		t.Fatalf("New() returned %T, want *serviceImpl", svc)
	}

	if ys.cacheSaveTimeout != 2*time.Second || ys.scraperPhaseTimeout != 9*time.Second {
		t.Fatalf("timeouts = (%s, %s), want (2s, 9s)", ys.cacheSaveTimeout, ys.scraperPhaseTimeout)
	}
}
