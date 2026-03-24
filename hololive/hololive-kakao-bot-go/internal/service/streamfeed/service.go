package streamfeed

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"golang.org/x/sync/errgroup"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/chzzk"
)

type orgStreamSource interface {
	GetLiveStreamsByOrg(ctx context.Context, org string) ([]*domain.Stream, error)
	GetUpcomingStreamsByOrg(ctx context.Context, hours int, org string) ([]*domain.Stream, error)
}

type chzzkScheduleClient interface {
	HasOpenAPICredentials() bool
	GetLivesByChannelIDs(ctx context.Context, channelIDs []string) ([]chzzk.LiveData, error)
	GetScheduledLives(ctx context.Context, channelID string) ([]chzzk.ScheduledLive, error)
}

type Service struct {
	streams orgStreamSource
	chzzk   chzzkScheduleClient
	members domain.MemberDataProvider
	logger  *slog.Logger

	stelliveMembersMu        sync.RWMutex
	stelliveMembers          []*domain.Member
	stelliveMembersExpiresAt time.Time
}

func NewService(streams orgStreamSource, chzzkClient chzzkScheduleClient, members domain.MemberDataProvider) *Service {
	return &Service{
		streams: streams,
		chzzk:   chzzkClient,
		members: members,
		logger:  slog.Default(),
	}
}

func (s *Service) GetLiveStreamsByOrg(ctx context.Context, org string) ([]*domain.Stream, error) {
	if s.streams == nil {
		return nil, fmt.Errorf("get live streams by org: stream source is nil")
	}

	liveStreams, err := s.streams.GetLiveStreamsByOrg(ctx, org)
	if err != nil {
		return nil, err
	}

	if !shouldMergeStellive(org) || s.chzzk == nil || s.members == nil || !s.chzzk.HasOpenAPICredentials() {
		return liveStreams, nil
	}

	members := s.getActiveStelliveMembers()
	if len(members) == 0 {
		return liveStreams, nil
	}

	channelIDs := make([]string, 0, len(members))
	for _, member := range members {
		channelIDs = append(channelIDs, member.ChzzkChannelID)
	}

	lives, err := s.chzzk.GetLivesByChannelIDs(ctx, channelIDs)
	if err != nil {
		s.logger.Warn("stream feed: chzzk live lookup failed", slog.Any("error", err))
		return liveStreams, nil
	}

	return mergeLiveStreams(liveStreams, members, lives), nil
}

func (s *Service) GetUpcomingStreamsByOrg(ctx context.Context, hours int, org string) ([]*domain.Stream, error) {
	if s.streams == nil {
		return nil, fmt.Errorf("get upcoming streams by org: stream source is nil")
	}

	upcomingStreams, err := s.streams.GetUpcomingStreamsByOrg(ctx, hours, org)
	if err != nil {
		return nil, err
	}

	if !shouldMergeStellive(org) || s.chzzk == nil || s.members == nil {
		return upcomingStreams, nil
	}

	members := s.getActiveStelliveMembers()
	if len(members) == 0 {
		return upcomingStreams, nil
	}

	var chzzkStreams []*domain.Stream
	var (
		mu sync.Mutex
		g  errgroup.Group
	)

	g.SetLimit(constants.ChzzkConfig.MaxConcurrentStatusChecks)
	for _, member := range members {
		member := member
		g.Go(func() error {
			scheduledLives, fetchErr := s.chzzk.GetScheduledLives(ctx, member.ChzzkChannelID)
			if fetchErr != nil {
				s.logger.Warn("stream feed: chzzk upcoming lookup failed",
					slog.String("channel_id", member.ChannelID),
					slog.String("chzzk_channel_id", member.ChzzkChannelID),
					slog.Any("error", fetchErr),
				)
				return nil
			}

			streams := buildUpcomingStreams(member, scheduledLives, hours)
			if len(streams) == 0 {
				return nil
			}

			mu.Lock()
			chzzkStreams = append(chzzkStreams, streams...)
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()

	if len(chzzkStreams) == 0 {
		return upcomingStreams, nil
	}

	return mergeUpcomingStreams(upcomingStreams, chzzkStreams), nil
}

func shouldMergeStellive(org string) bool {
	normalized := strings.ToLower(strings.TrimSpace(org))
	return normalized == strings.ToLower(constants.HolodexAPIParams.OrgStellive) || normalized == constants.HolodexAPIParams.OrgAll
}

func (s *Service) getActiveStelliveMembers() []*domain.Member {
	if s == nil || s.members == nil {
		return nil
	}

	now := time.Now()

	s.stelliveMembersMu.RLock()
	if now.Before(s.stelliveMembersExpiresAt) && s.stelliveMembers != nil {
		members := cloneMembers(s.stelliveMembers)
		s.stelliveMembersMu.RUnlock()
		return members
	}
	s.stelliveMembersMu.RUnlock()

	s.stelliveMembersMu.Lock()
	defer s.stelliveMembersMu.Unlock()

	if time.Now().Before(s.stelliveMembersExpiresAt) && s.stelliveMembers != nil {
		return cloneMembers(s.stelliveMembers)
	}

	filtered := activeStelliveMembers(s.members.GetAllMembers())
	s.stelliveMembers = cloneMembers(filtered)
	s.stelliveMembersExpiresAt = time.Now().Add(constants.CacheTTL.ChannelSchedule)
	return cloneMembers(s.stelliveMembers)
}

func cloneMembers(members []*domain.Member) []*domain.Member {
	if len(members) == 0 {
		return nil
	}

	cloned := make([]*domain.Member, len(members))
	copy(cloned, members)
	return cloned
}

func activeStelliveMembers(members []*domain.Member) []*domain.Member {
	if len(members) == 0 {
		return nil
	}

	filtered := make([]*domain.Member, 0, len(members))
	for _, member := range members {
		if member == nil || member.IsGraduated || member.ChzzkChannelID == "" {
			continue
		}
		if !strings.EqualFold(member.Org, constants.HolodexAPIParams.OrgStellive) {
			continue
		}
		filtered = append(filtered, member)
	}

	return filtered
}

func mergeLiveStreams(base []*domain.Stream, members []*domain.Member, lives []chzzk.LiveData) []*domain.Stream {
	merged := append([]*domain.Stream(nil), base...)
	memberByChzzk := make(map[string]*domain.Member, len(members))
	for _, member := range members {
		memberByChzzk[member.ChzzkChannelID] = member
	}

	for _, live := range lives {
		member := memberByChzzk[live.ChannelID]
		if member == nil {
			continue
		}

		if existing := findLiveStreamByChannel(merged, member.ChannelID); existing != nil {
			existing.ChzzkChannelID = member.ChzzkChannelID
			existing.ChzzkLiveURL = fmt.Sprintf("https://chzzk.naver.com/live/%s", member.ChzzkChannelID)
			existing.IsIntegrated = existing.HasYouTubeInfo()
			if !existing.HasYouTubeInfo() {
				existing.IsChzzkOnly = true
				link := existing.ChzzkLiveURL
				existing.Link = &link
			}
			continue
		}

		liveURL := fmt.Sprintf("https://chzzk.naver.com/live/%s", member.ChzzkChannelID)
		thumbnail := live.LiveThumbnailImageURL
		link := liveURL
		merged = append(merged, &domain.Stream{
			Title:          live.LiveTitle,
			ChannelID:      member.ChannelID,
			ChannelName:    member.Name,
			Status:         domain.StreamStatusLive,
			Thumbnail:      &thumbnail,
			Link:           &link,
			ChzzkChannelID: member.ChzzkChannelID,
			ChzzkLiveURL:   liveURL,
			IsChzzkOnly:    true,
		})
	}

	return merged
}

func buildUpcomingStreams(member *domain.Member, scheduledLives []chzzk.ScheduledLive, hours int) []*domain.Stream {
	if member == nil || len(scheduledLives) == 0 {
		return nil
	}

	now := time.Now()
	cutoff := now.Add(time.Duration(hours) * time.Hour)
	liveURL := fmt.Sprintf("https://chzzk.naver.com/live/%s", member.ChzzkChannelID)
	streams := make([]*domain.Stream, 0, len(scheduledLives))

	for _, scheduledLive := range scheduledLives {
		startAt, err := chzzk.ParseScheduledStartAt(scheduledLive.ScheduledStartAt)
		if err != nil {
			continue
		}
		if !startAt.After(now) || startAt.After(cutoff) {
			continue
		}

		link := liveURL
		streams = append(streams, &domain.Stream{
			Title:          scheduledLive.LiveTitle,
			ChannelID:      member.ChannelID,
			ChannelName:    member.Name,
			Status:         domain.StreamStatusUpcoming,
			StartScheduled: &startAt,
			Link:           &link,
			ChzzkChannelID: member.ChzzkChannelID,
			ChzzkLiveURL:   liveURL,
			IsChzzkOnly:    true,
		})
	}

	return streams
}

func mergeUpcomingStreams(base []*domain.Stream, additions []*domain.Stream) []*domain.Stream {
	merged := append([]*domain.Stream(nil), base...)

	for _, addition := range additions {
		if addition == nil {
			continue
		}

		if existing := findUpcomingStreamMatch(merged, addition); existing != nil {
			existing.ChzzkChannelID = addition.ChzzkChannelID
			existing.ChzzkLiveURL = addition.ChzzkLiveURL
			if existing.HasYouTubeInfo() {
				existing.IsIntegrated = true
				existing.IsChzzkOnly = false
			} else {
				existing.IsChzzkOnly = true
				existing.Link = addition.Link
			}
			continue
		}

		merged = append(merged, addition)
	}

	slices.SortStableFunc(merged, func(a, b *domain.Stream) int {
		switch {
		case a == nil && b == nil:
			return 0
		case a == nil:
			return 1
		case b == nil:
			return -1
		case a.StartScheduled == nil && b.StartScheduled == nil:
			return 0
		case a.StartScheduled == nil:
			return 1
		case b.StartScheduled == nil:
			return -1
		case a.StartScheduled.Before(*b.StartScheduled):
			return -1
		case a.StartScheduled.After(*b.StartScheduled):
			return 1
		default:
			return 0
		}
	})

	return merged
}

func findLiveStreamByChannel(streams []*domain.Stream, channelID string) *domain.Stream {
	for _, stream := range streams {
		if stream != nil && stream.ChannelID == channelID && stream.IsLive() {
			return stream
		}
	}
	return nil
}

func findUpcomingStreamMatch(streams []*domain.Stream, candidate *domain.Stream) *domain.Stream {
	if candidate == nil || candidate.StartScheduled == nil {
		return nil
	}

	for _, stream := range streams {
		if stream == nil || stream.StartScheduled == nil {
			continue
		}
		if stream.ChannelID != candidate.ChannelID {
			continue
		}
		if stream.StartScheduled.UTC().Unix()/60 == candidate.StartScheduled.UTC().Unix()/60 {
			return stream
		}
	}

	return nil
}
