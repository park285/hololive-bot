package streamfeed

import (
	"context"
	"log/slog"
	"sync"

	"github.com/kapu/hololive-shared/pkg/config"
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
	if !s.canMergeStelliveUpcomingStreams(org) {
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

	g.SetLimit(config.DefaultChzzkOperationalConfig().MaxConcurrentStatusChecks)
	for _, member := range members {
		g.Go(func() error {
			streams := s.fetchStelliveUpcomingStreams(ctx, member, hours)
			appendStelliveUpcomingStreams(&mu, &chzzkStreams, streams)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		s.logger.Warn("stream feed: chzzk upcoming merge failed", slog.Any("error", err))
	}

	if len(chzzkStreams) == 0 {
		return upcomingStreams
	}

	return mergeUpcomingStreams(upcomingStreams, chzzkStreams)
}

func (s *Service) canMergeStelliveUpcomingStreams(org string) bool {
	return shouldMergeStellive(org) && s.chzzk != nil && s.members != nil
}

func (s *Service) fetchStelliveUpcomingStreams(ctx context.Context, member *domain.Member, hours int) []*domain.Stream {
	scheduledLives, err := s.chzzk.GetScheduledLives(ctx, member.ChzzkChannelID)
	if err != nil {
		s.logger.Warn("stream feed: chzzk upcoming lookup failed",
			slog.String("channel_id", member.ChannelID),
			slog.String("chzzk_channel_id", member.ChzzkChannelID),
			slog.Any("error", err),
		)
		return nil
	}

	return buildUpcomingStreams(member, scheduledLives, hours)
}

func appendStelliveUpcomingStreams(mu *sync.Mutex, streams *[]*domain.Stream, additions []*domain.Stream) {
	if len(additions) == 0 {
		return
	}

	mu.Lock()
	*streams = append(*streams, additions...)
	mu.Unlock()
}
