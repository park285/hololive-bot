package checker

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
)

func (c *YouTubeChecker) loadDueYouTubeCheckInputs(
	ctx context.Context,
	now time.Time,
) ([]string, map[string][]*domain.Stream, map[string]time.Time, map[string][]string, error) {
	channelIDs, err := c.cacheSvc.SMembers(ctx, sharedalarmkeys.AlarmChannelRegistryKey)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("check youtube streams: read channel registry: %w", err)
	}

	if len(channelIDs) == 0 {
		return nil, nil, nil, nil, nil
	}

	dueChannels := c.tierScheduler.SelectDueChannels(channelIDs)
	if len(dueChannels) == 0 {
		return nil, nil, nil, nil, nil
	}
	sort.Strings(dueChannels)

	streamsByChannel, holodexErr := c.loadHolodexStreamsByChannel(ctx, dueChannels)
	persistedSessions, persistedErr := c.loadPersistedLiveSessions(ctx, dueChannels, now)
	if persistedErr != nil {
		c.logPersistedLiveSourceError(persistedErr)
	}
	if c.shouldFailAfterHolodexError(holodexErr, persistedErr, persistedSessions) {
		return nil, nil, nil, nil, fmt.Errorf("check youtube streams: fetch channels live status: %w", holodexErr)
	}
	liveObservedAtByStreamID := mergePersistedLiveSessionStreams(streamsByChannel, persistedSessions)

	subscriberMap, err := loadSubscriberRoomsByChannel(ctx, c.cacheSvc, dueChannels)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("check youtube streams: load subscriber rooms: %w", err)
	}
	c.observePersistedLiveGuardrails(ctx, persistedSessions, subscriberMap, now)

	return dueChannels, streamsByChannel, liveObservedAtByStreamID, subscriberMap, nil
}

func (c *YouTubeChecker) loadHolodexStreamsByChannel(
	ctx context.Context,
	dueChannels []string,
) (map[string][]*domain.Stream, error) {
	streams, err := c.holodexSvc.GetChannelsLiveStatus(ctx, dueChannels)
	if err == nil {
		return groupStreamsByChannel(streams), nil
	}
	if c.persistedLiveSource != nil {
		c.logger.Warn("YouTube Holodex live status source failed; continuing with persisted live sessions",
			slog.Any("error", err),
		)
	}
	return map[string][]*domain.Stream{}, err
}

func (c *YouTubeChecker) shouldFailAfterHolodexError(
	holodexErr error,
	persistedErr error,
	persistedSessions []PersistedYouTubeLiveSession,
) bool {
	if holodexErr == nil {
		return false
	}
	return c.persistedLiveSource == nil || persistedErr != nil || len(persistedSessions) == 0
}
