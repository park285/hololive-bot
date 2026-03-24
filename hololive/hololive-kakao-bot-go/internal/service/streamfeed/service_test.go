package streamfeed

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/chzzk"
)

type stubOrgStreamSource struct {
	liveStreams     []*domain.Stream
	liveErr         error
	upcomingStreams []*domain.Stream
	upcomingErr     error
}

func (s *stubOrgStreamSource) GetLiveStreamsByOrg(context.Context, string) ([]*domain.Stream, error) {
	return s.liveStreams, s.liveErr
}

func (s *stubOrgStreamSource) GetUpcomingStreamsByOrg(context.Context, int, string) ([]*domain.Stream, error) {
	return s.upcomingStreams, s.upcomingErr
}

type stubMemberProvider struct {
	members      []*domain.Member
	getAllCalls  atomic.Int32
}

func (s *stubMemberProvider) FindMemberByChannelID(channelID string) *domain.Member {
	for _, member := range s.members {
		if member != nil && member.ChannelID == channelID {
			return member
		}
	}
	return nil
}

func (s *stubMemberProvider) FindMemberByName(string) *domain.Member                { return nil }
func (s *stubMemberProvider) FindMemberByAlias(string) *domain.Member               { return nil }
func (s *stubMemberProvider) GetChannelIDs() []string                               { return nil }
func (s *stubMemberProvider) GetAllMembers() []*domain.Member {
	s.getAllCalls.Add(1)
	return s.members
}
func (s *stubMemberProvider) WithContext(context.Context) domain.MemberDataProvider { return s }
func (s *stubMemberProvider) FindMembersByName(string) []*domain.Member             { return nil }
func (s *stubMemberProvider) FindMembersByAlias(string) []*domain.Member            { return nil }

type stubChzzkClient struct {
	lives          []chzzk.LiveData
	scheduledLives map[string][]chzzk.ScheduledLive
	scheduledDelay map[string]chan struct{}
	scheduledCalls atomic.Int32
	maxInFlight    atomic.Int32
	inFlight       atomic.Int32
}

func (s *stubChzzkClient) HasOpenAPICredentials() bool { return true }

func (s *stubChzzkClient) GetLivesByChannelIDs(context.Context, []string) ([]chzzk.LiveData, error) {
	return s.lives, nil
}

func (s *stubChzzkClient) GetScheduledLives(_ context.Context, channelID string) ([]chzzk.ScheduledLive, error) {
	s.scheduledCalls.Add(1)
	current := s.inFlight.Add(1)
	for {
		maxSeen := s.maxInFlight.Load()
		if current <= maxSeen {
			break
		}
		if s.maxInFlight.CompareAndSwap(maxSeen, current) {
			break
		}
	}
	defer s.inFlight.Add(-1)

	if gate := s.scheduledDelay[channelID]; gate != nil {
		<-gate
	}

	return s.scheduledLives[channelID], nil
}

func TestServiceGetLiveStreamsByOrg_StelliveIncludesChzzkLive(t *testing.T) {
	t.Parallel()

	service := NewService(
		&stubOrgStreamSource{},
		&stubChzzkClient{
			lives: []chzzk.LiveData{
				{
					ChannelID:             "cz-1",
					LiveTitle:             "치지직 라이브",
					LiveThumbnailImageURL: "https://example.com/live.jpg",
				},
			},
		},
		&stubMemberProvider{
			members: []*domain.Member{
				{
					ChannelID:      "yt-1",
					Name:           "유니",
					Org:            constants.HolodexAPIParams.OrgStellive,
					ChzzkChannelID: "cz-1",
				},
			},
		},
	)

	streams, err := service.GetLiveStreamsByOrg(t.Context(), "stellive")
	if err != nil {
		t.Fatalf("GetLiveStreamsByOrg() error = %v", err)
	}

	if len(streams) != 1 {
		t.Fatalf("len(streams) = %d, want 1", len(streams))
	}

	if streams[0].GetYouTubeURL() != "https://chzzk.naver.com/live/cz-1" {
		t.Fatalf("url = %q, want chzzk live url", streams[0].GetYouTubeURL())
	}
}

func TestServiceGetUpcomingStreamsByOrg_AllIncludesStelliveSchedules(t *testing.T) {
	t.Parallel()

	now := time.Now().In(time.FixedZone("KST", 9*60*60))
	scheduled := now.Add(2 * time.Hour).Format(chzzk.ChzzkTimeLayout)

	service := NewService(
		&stubOrgStreamSource{},
		&stubChzzkClient{
			scheduledLives: map[string][]chzzk.ScheduledLive{
				"cz-1": {
					{
						LiveTitle:        "치지직 예정 방송",
						ScheduledStartAt: scheduled,
					},
				},
			},
		},
		&stubMemberProvider{
			members: []*domain.Member{
				{
					ChannelID:      "yt-1",
					Name:           "유니",
					Org:            constants.HolodexAPIParams.OrgStellive,
					ChzzkChannelID: "cz-1",
				},
			},
		},
	)

	streams, err := service.GetUpcomingStreamsByOrg(t.Context(), 24, constants.HolodexAPIParams.OrgAll)
	if err != nil {
		t.Fatalf("GetUpcomingStreamsByOrg() error = %v", err)
	}

	if len(streams) != 1 {
		t.Fatalf("len(streams) = %d, want 1", len(streams))
	}

	if streams[0].ChannelID != "yt-1" {
		t.Fatalf("ChannelID = %q, want yt-1", streams[0].ChannelID)
	}

	if streams[0].GetYouTubeURL() != "https://chzzk.naver.com/live/cz-1" {
		t.Fatalf("url = %q, want chzzk live url", streams[0].GetYouTubeURL())
	}
}

func TestServiceStelliveMerge_ReusesFilteredMembersAcrossRequests(t *testing.T) {
	t.Parallel()

	now := time.Now().In(time.FixedZone("KST", 9*60*60))
	scheduled := now.Add(2 * time.Hour).Format(chzzk.ChzzkTimeLayout)

	memberProvider := &stubMemberProvider{
		members: []*domain.Member{
			{
				ChannelID:      "yt-1",
				Name:           "유니",
				Org:            constants.HolodexAPIParams.OrgStellive,
				ChzzkChannelID: "cz-1",
			},
		},
	}

	service := NewService(
		&stubOrgStreamSource{},
		&stubChzzkClient{
			lives: []chzzk.LiveData{
				{
					ChannelID:             "cz-1",
					LiveTitle:             "치지직 라이브",
					LiveThumbnailImageURL: "https://example.com/live.jpg",
				},
			},
			scheduledLives: map[string][]chzzk.ScheduledLive{
				"cz-1": {
					{
						LiveTitle:        "치지직 예정 방송",
						ScheduledStartAt: scheduled,
					},
				},
			},
		},
		memberProvider,
	)

	if _, err := service.GetLiveStreamsByOrg(t.Context(), constants.HolodexAPIParams.OrgStellive); err != nil {
		t.Fatalf("GetLiveStreamsByOrg() error = %v", err)
	}
	if _, err := service.GetUpcomingStreamsByOrg(t.Context(), 24, constants.HolodexAPIParams.OrgStellive); err != nil {
		t.Fatalf("GetUpcomingStreamsByOrg() error = %v", err)
	}

	if got := memberProvider.getAllCalls.Load(); got != 1 {
		t.Fatalf("GetAllMembers() calls = %d, want 1", got)
	}
}

func TestServiceGetUpcomingStreamsByOrg_StelliveScheduleLookupsRunConcurrently(t *testing.T) {
	t.Parallel()

	now := time.Now().In(time.FixedZone("KST", 9*60*60))
	scheduled1 := now.Add(2 * time.Hour).Format(chzzk.ChzzkTimeLayout)
	scheduled2 := now.Add(3 * time.Hour).Format(chzzk.ChzzkTimeLayout)

	chzzkClient := &stubChzzkClient{
		scheduledLives: map[string][]chzzk.ScheduledLive{
			"cz-1": {{LiveTitle: "방송 1", ScheduledStartAt: scheduled1}},
			"cz-2": {{LiveTitle: "방송 2", ScheduledStartAt: scheduled2}},
		},
		scheduledDelay: map[string]chan struct{}{
			"cz-1": make(chan struct{}),
			"cz-2": make(chan struct{}),
		},
	}

	service := NewService(
		&stubOrgStreamSource{},
		chzzkClient,
		&stubMemberProvider{
			members: []*domain.Member{
				{
					ChannelID:      "yt-1",
					Name:           "유니",
					Org:            constants.HolodexAPIParams.OrgStellive,
					ChzzkChannelID: "cz-1",
				},
				{
					ChannelID:      "yt-2",
					Name:           "칸나",
					Org:            constants.HolodexAPIParams.OrgStellive,
					ChzzkChannelID: "cz-2",
				},
			},
		},
	)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = service.GetUpcomingStreamsByOrg(context.Background(), 24, constants.HolodexAPIParams.OrgStellive)
	}()

	deadline := time.After(200 * time.Millisecond)
	for chzzkClient.scheduledCalls.Load() < 2 {
		select {
		case <-deadline:
			close(chzzkClient.scheduledDelay["cz-1"])
			close(chzzkClient.scheduledDelay["cz-2"])
			<-done
			t.Fatal("expected both Chzzk schedule lookups to start before the first one completed")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	close(chzzkClient.scheduledDelay["cz-1"])
	close(chzzkClient.scheduledDelay["cz-2"])
	<-done

	if got := chzzkClient.maxInFlight.Load(); got < 2 {
		t.Fatalf("max concurrent scheduled lookups = %d, want >= 2", got)
	}
}
