package streamfeed

import (
	"context"
	"log/slog"
	"sync"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"golang.org/x/sync/errgroup"
)

func (s *Service) mergeStelliveLiveStreams(ctx context.Context, org string, liveStreams []*domain.Stream) []*domain.Stream {
	if !shouldMergeStellive(org) || s.chzzk == nil || s.members == nil || !s.chzzk.HasOpenAPICredentials() {
		return liveStreams
	}

	members := s.getActiveStelliveMembers()
	if len(members) == 0 {
		return liveStreams
	}

	channelIDs := make([]string, 0, len(members))
	for _, member := range members {
		channelIDs = append(channelIDs, member.ChzzkChannelID)
	}

	lives, err := s.chzzk.GetLivesByChannelIDs(ctx, channelIDs)
	if err != nil {
		s.logger.Warn("stream feed: chzzk live lookup failed", slog.Any("error", err))
		return liveStreams
	}

	return mergeLiveStreams(liveStreams, members, lives)
}

func (s *Service) mergeStelliveUpcomingStreams(ctx context.Context, org string, hours int, upcomingStreams []*domain.Stream) []*domain.Stream {
	if !shouldMergeStellive(org) || s.chzzk == nil || s.members == nil {
		return upcomingStreams
	}

	members := s.getActiveStelliveMembers()
	if len(members) == 0 {
		return upcomingStreams
	}

	var chzzkStreams []*domain.Stream
	var (
		mu sync.Mutex
		g  errgroup.Group
	)

	g.SetLimit(constants.ChzzkConfig.MaxConcurrentStatusChecks)
	for _, member := range members {
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
		return upcomingStreams
	}

	return mergeUpcomingStreams(upcomingStreams, chzzkStreams)
}
