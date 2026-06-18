package streamschedule

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/streamcommon"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
)

type Service struct {
	holodex domain.StreamProvider
	chzzk   *chzzk.Client
	members domain.MemberDataProvider
	logger  *slog.Logger
}

func NewService(
	holodex domain.StreamProvider,
	chzzkClient *chzzk.Client,
	members domain.MemberDataProvider,
	logger *slog.Logger,
) *Service {
	if logger == nil {
		logger = slog.Default()
	}

	return &Service{
		holodex: holodex,
		chzzk:   chzzkClient,
		members: members,
		logger:  logger,
	}
}

func (s *Service) GetChannelSchedule(ctx context.Context, channelID string, hours int, includeLive bool) ([]*domain.Stream, error) {
	if s.holodex == nil {
		return nil, fmt.Errorf("get channel schedule: holodex is nil")
	}

	streams, err := s.holodex.GetChannelSchedule(ctx, channelID, hours, includeLive)
	if err != nil {
		return nil, err
	}

	chzzkStreams := s.getChzzkScheduleStreams(ctx, channelID, hours)
	if len(chzzkStreams) == 0 {
		return streams, nil
	}

	return mergeScheduleStreams(streams, chzzkStreams), nil
}

func (s *Service) getChzzkScheduleStreams(ctx context.Context, channelID string, hours int) []*domain.Stream {
	if s.chzzk == nil || s.members == nil {
		return nil
	}

	member := s.members.FindMemberByChannelID(channelID)
	if member == nil || !canLoadChzzkSchedule(member) {
		return nil
	}

	scheduledLives, err := s.chzzk.GetScheduledLives(ctx, member.ChzzkChannelID)
	if err != nil {
		s.logger.Warn("stream schedule: chzzk schedule lookup failed",
			slog.String("channel_id", channelID),
			slog.String("chzzk_channel_id", member.ChzzkChannelID),
			slog.Any("error", err),
		)
		return nil
	}

	return buildChzzkScheduleStreams(member, scheduledLives, hours)
}

func canLoadChzzkSchedule(member *domain.Member) bool {
	return member != nil && member.ChzzkChannelID != "" && !member.IsGraduated
}

func buildChzzkScheduleStreams(member *domain.Member, scheduledLives []chzzk.ScheduledLive, hours int) []*domain.Stream {
	if member == nil || member.ChzzkChannelID == "" || len(scheduledLives) == 0 {
		return nil
	}

	now := time.Now()
	cutoff := now.Add(time.Duration(hours) * time.Hour)
	liveURL := member.GetChzzkLiveURL()
	streams := make([]*domain.Stream, 0, len(scheduledLives))

	for _, scheduledLive := range scheduledLives {
		startAt, err := chzzk.ParseScheduledStartAt(scheduledLive.ScheduledStartAt)
		if err != nil || !isChzzkScheduleInWindow(startAt, now, cutoff) {
			continue
		}

		streams = append(streams, buildChzzkScheduleStream(member, scheduledLive, startAt, liveURL))
	}

	return streams
}

func isChzzkScheduleInWindow(startAt, now, cutoff time.Time) bool {
	return startAt.After(now) && !startAt.After(cutoff)
}

func buildChzzkScheduleStream(
	member *domain.Member,
	scheduledLive chzzk.ScheduledLive,
	startAt time.Time,
	liveURL string,
) *domain.Stream {
	link := liveURL
	org := member.GetOrg()
	return &domain.Stream{
		ID:             buildChzzkScheduleStreamID(member.ChzzkChannelID, scheduledLive.LiveTitle, startAt),
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
	}
}

func mergeScheduleStreams(baseStreams, chzzkStreams []*domain.Stream) []*domain.Stream {
	merged := append([]*domain.Stream(nil), baseStreams...)

	for _, chzzkStream := range chzzkStreams {
		if chzzkStream == nil {
			continue
		}

		if existing := streamcommon.FindByChannelAndScheduledMinute(merged, chzzkStream); existing != nil {
			existing.ChzzkChannelID = chzzkStream.ChzzkChannelID
			existing.ChzzkLiveURL = chzzkStream.ChzzkLiveURL

			if existing.HasYouTubeInfo() {
				existing.IsIntegrated = true
				existing.IsChzzkOnly = false
			} else {
				existing.IsChzzkOnly = true
				existing.Link = chzzkStream.Link
			}

			continue
		}

		merged = append(merged, chzzkStream)
	}

	slices.SortStableFunc(merged, compareScheduleStreams)

	return merged
}

func compareScheduleStreams(a, b *domain.Stream) int {
	if result, ok := compareNilScheduleStreams(a, b); ok {
		return result
	}
	if result, ok := compareLiveScheduleStreams(a, b); ok {
		return result
	}

	return compareScheduledStart(a.StartScheduled, b.StartScheduled)
}

func compareNilScheduleStreams(a, b *domain.Stream) (int, bool) {
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

func compareLiveScheduleStreams(a, b *domain.Stream) (int, bool) {
	if a.IsLive() != b.IsLive() {
		if a.IsLive() {
			return -1, true
		}
		return 1, true
	}

	return 0, false
}

func compareScheduledStart(a, b *time.Time) int {
	if result, ok := compareNilScheduledStart(a, b); ok {
		return result
	}

	return compareScheduledStartTime(*a, *b)
}

func compareNilScheduledStart(a, b *time.Time) (int, bool) {
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

func compareScheduledStartTime(a, b time.Time) int {
	if a.Before(b) {
		return -1
	}
	if a.After(b) {
		return 1
	}
	return 0
}

func buildChzzkScheduleStreamID(chzzkChannelID, title string, startAt time.Time) string {
	seed := strings.Join([]string{
		strings.TrimSpace(chzzkChannelID),
		strings.TrimSpace(title),
		startAt.UTC().Format(time.RFC3339),
	}, "|")
	sum := sha256.Sum256([]byte(seed))
	return fmt.Sprintf("chzzk:%s:schedule:%x", strings.TrimSpace(chzzkChannelID), sum[:8])
}
