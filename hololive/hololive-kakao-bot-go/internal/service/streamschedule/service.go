package streamschedule

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/chzzk"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/streamcommon"
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

	if s.chzzk == nil || s.members == nil {
		return streams, nil
	}

	member := s.members.FindMemberByChannelID(channelID)
	if member == nil || member.ChzzkChannelID == "" || member.IsGraduated {
		return streams, nil
	}

	scheduledLives, err := s.chzzk.GetScheduledLives(ctx, member.ChzzkChannelID)
	if err != nil {
		s.logger.Warn("stream schedule: chzzk schedule lookup failed",
			slog.String("channel_id", channelID),
			slog.String("chzzk_channel_id", member.ChzzkChannelID),
			slog.Any("error", err),
		)
		return streams, nil
	}

	chzzkStreams := buildChzzkScheduleStreams(member, scheduledLives, hours)
	if len(chzzkStreams) == 0 {
		return streams, nil
	}

	return mergeScheduleStreams(streams, chzzkStreams), nil
}

func buildChzzkScheduleStreams(member *domain.Member, scheduledLives []chzzk.ScheduledLive, hours int) []*domain.Stream {
	if member == nil || member.ChzzkChannelID == "" || len(scheduledLives) == 0 {
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
	if a == nil && b == nil {
		return 0
	}
	if a == nil {
		return 1
	}
	if b == nil {
		return -1
	}

	if a.IsLive() != b.IsLive() {
		if a.IsLive() {
			return -1
		}
		return 1
	}

	switch {
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
}
