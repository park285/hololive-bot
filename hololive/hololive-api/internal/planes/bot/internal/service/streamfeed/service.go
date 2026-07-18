package streamfeed

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/streamcommon"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
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

	return s.mergeStelliveLiveStreams(ctx, org, liveStreams), nil
}

func (s *Service) GetUpcomingStreamsByOrg(ctx context.Context, hours int, org string) ([]*domain.Stream, error) {
	if s.streams == nil {
		return nil, fmt.Errorf("get upcoming streams by org: stream source is nil")
	}

	upcomingStreams, err := s.streams.GetUpcomingStreamsByOrg(ctx, hours, org)
	if err != nil {
		return nil, err
	}

	return s.mergeStelliveUpcomingStreams(ctx, org, hours, upcomingStreams), nil
}

func shouldMergeStellive(org string) bool {
	normalized := strings.ToLower(strings.TrimSpace(org))
	return strings.EqualFold(normalized, constants.HolodexAPIParams.OrgStellive) || normalized == constants.HolodexAPIParams.OrgAll
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

	return slices.Clone(members)
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
	merged := slices.Clone(base)
	memberByChzzk := membersByChzzkChannelID(members)

	for i := range lives {
		live := &lives[i]
		merged = mergeLiveStream(merged, memberByChzzk[live.ChannelID], live)
	}

	return merged
}

func membersByChzzkChannelID(members []*domain.Member) map[string]*domain.Member {
	memberByChzzk := make(map[string]*domain.Member, len(members))
	for _, member := range members {
		memberByChzzk[member.ChzzkChannelID] = member
	}
	return memberByChzzk
}

func mergeLiveStream(merged []*domain.Stream, member *domain.Member, live *chzzk.LiveData) []*domain.Stream {
	if member == nil {
		return merged
	}
	if existing := findLiveStreamByChannel(merged, member.ChannelID); existing != nil {
		updateExistingLiveStream(existing, member)
		return merged
	}
	return append(merged, buildChzzkLiveStream(member, live))
}

func updateExistingLiveStream(existing *domain.Stream, member *domain.Member) {
	existing.ChzzkChannelID = member.ChzzkChannelID
	existing.ChzzkLiveURL = member.GetChzzkLiveURL()
	existing.IsIntegrated = existing.HasYouTubeInfo()
	if !existing.HasYouTubeInfo() {
		existing.IsChzzkOnly = true
		link := existing.ChzzkLiveURL
		existing.Link = &link
	}
}

func buildChzzkLiveStream(member *domain.Member, live *chzzk.LiveData) *domain.Stream {
	if live == nil {
		return nil
	}
	liveURL := member.GetChzzkLiveURL()
	thumbnail := live.LiveThumbnailImageURL
	link := liveURL
	now := time.Now().UTC().Truncate(time.Minute)
	org := member.GetOrg()
	return &domain.Stream{
		ID:             buildChzzkStreamID(member.ChzzkChannelID, "live", live.LiveTitle, now),
		Title:          live.LiveTitle,
		ChannelID:      member.ChannelID,
		ChannelName:    member.Name,
		Status:         domain.StreamStatusLive,
		StartScheduled: &now,
		StartActual:    &now,
		Thumbnail:      &thumbnail,
		Link:           &link,
		Channel: &domain.Channel{
			ID:   member.ChannelID,
			Name: member.Name,
			Org:  &org,
		},
		ChzzkChannelID: member.ChzzkChannelID,
		ChzzkLiveURL:   liveURL,
		IsChzzkOnly:    true,
	}
}

func buildUpcomingStreams(member *domain.Member, scheduledLives []chzzk.ScheduledLive, hours int) []*domain.Stream {
	if member == nil || len(scheduledLives) == 0 {
		return nil
	}

	now := time.Now()
	cutoff := now.Add(time.Duration(hours) * time.Hour)
	liveURL := member.GetChzzkLiveURL()
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
		org := member.GetOrg()
		streams = append(streams, &domain.Stream{
			ID:             buildChzzkStreamID(member.ChzzkChannelID, "schedule", scheduledLive.LiveTitle, startAt),
			Title:          scheduledLive.LiveTitle,
			ChannelID:      member.ChannelID,
			ChannelName:    member.Name,
			Status:         domain.StreamStatusUpcoming,
			StartScheduled: &startAt,
			Link:           &link,
			Channel: &domain.Channel{
				ID:   member.ChannelID,
				Name: member.Name,
				Org:  &org,
			},
			ChzzkChannelID: member.ChzzkChannelID,
			ChzzkLiveURL:   liveURL,
			IsChzzkOnly:    true,
		})
	}

	return streams
}

func mergeUpcomingStreams(base, additions []*domain.Stream) []*domain.Stream {
	merged := slices.Clone(base)

	for _, addition := range additions {
		merged = mergeUpcomingStream(merged, addition)
	}

	slices.SortStableFunc(merged, compareUpcomingStreams)
	return merged
}

func mergeUpcomingStream(merged []*domain.Stream, addition *domain.Stream) []*domain.Stream {
	if addition == nil {
		return merged
	}
	if existing := streamcommon.FindByChannelAndScheduledMinute(merged, addition); existing != nil {
		updateExistingUpcomingStream(existing, addition)
		return merged
	}
	return append(merged, addition)
}

func updateExistingUpcomingStream(existing, addition *domain.Stream) {
	existing.ChzzkChannelID = addition.ChzzkChannelID
	existing.ChzzkLiveURL = addition.ChzzkLiveURL
	if existing.HasYouTubeInfo() {
		existing.IsIntegrated = true
		existing.IsChzzkOnly = false
		return
	}
	existing.IsChzzkOnly = true
	existing.Link = addition.Link
}

func compareUpcomingStreams(a, b *domain.Stream) int {
	if result, ok := compareNilStreams(a, b); ok {
		return result
	}
	return compareOptionalTime(a.StartScheduled, b.StartScheduled)
}

func compareNilStreams(a, b *domain.Stream) (int, bool) {
	if a == nil && b == nil {
		return 0, true
	}
	if a == nil {
		return 1, true
	}
	if b == nil {
		return -1, true
	}
	return 0, false
}

func compareOptionalTime(a, b *time.Time) int {
	if a == nil && b == nil {
		return 0
	}
	if a == nil {
		return 1
	}
	if b == nil {
		return -1
	}
	if a.Before(*b) {
		return -1
	}
	if a.After(*b) {
		return 1
	}
	return 0
}

func findLiveStreamByChannel(streams []*domain.Stream, channelID string) *domain.Stream {
	for _, stream := range streams {
		if stream != nil && stream.ChannelID == channelID && stream.IsLive() {
			return stream
		}
	}
	return nil
}

// 피드 표시용 임시 ID로, live는 분 단위 bucket이라 값이 매분 바뀐다. dedup/저장 키 사용 금지 —
// alarm-worker의 stable identity(chzzk_checker.go)와 다른 것이 의도다.
func buildChzzkStreamID(chzzkChannelID, kind, title string, at time.Time) string {
	seed := strings.Join([]string{
		strings.TrimSpace(chzzkChannelID),
		strings.TrimSpace(kind),
		strings.TrimSpace(title),
		at.UTC().Format(time.RFC3339),
	}, "|")
	sum := sha256.Sum256([]byte(seed))
	return fmt.Sprintf("chzzk:%s:%s:%x", strings.TrimSpace(chzzkChannelID), strings.TrimSpace(kind), sum[:8])
}
