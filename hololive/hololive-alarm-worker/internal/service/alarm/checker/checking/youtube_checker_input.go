package checking

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
)

type youtubeLiveCheckEvidence struct {
	currentProviderStreams map[string][]*domain.Stream
	observedAtByStreamID   map[string]time.Time
	sentRoomsByStreamID    map[string]map[string]struct{}
}

func (c *YouTubeChecker) loadDueYouTubeCheckInputs(
	ctx context.Context,
	now time.Time,
) (dueChannels []string, streamsByChannel map[string][]*domain.Stream, liveEvidence youtubeLiveCheckEvidence, subscriberMap map[string][]string, err error) {
	channelIDs, err := c.cacheClient.SMembers(ctx, sharedalarmkeys.AlarmChannelRegistryKey)
	if err != nil {
		return nil, nil, youtubeLiveCheckEvidence{}, nil, fmt.Errorf("check youtube streams: read channel registry: %w", err)
	}

	if len(channelIDs) == 0 {
		return nil, map[string][]*domain.Stream{}, youtubeLiveCheckEvidence{}, map[string][]string{}, nil
	}

	dueChannels = c.selectDueYouTubeChannels(ctx, channelIDs, time.Now().UTC())

	if len(dueChannels) == 0 {
		return nil, map[string][]*domain.Stream{}, youtubeLiveCheckEvidence{}, map[string][]string{}, nil
	}

	slices.Sort(dueChannels)

	streamsByChannel, holodexErr := c.loadHolodexStreamsByChannel(ctx, dueChannels)
	if streamsByChannel == nil {
		// Holodex가 실패해도 persisted 세션을 이 맵에 병합해야 하므로, 여기서 비어 있는 맵을 마련해 둔다.
		streamsByChannel = make(map[string][]*domain.Stream)
	}

	if holodexErr == nil {
		// 과거 canonical 보강이 최신 provider 응답으로 위장해 후보를 취소하지 않도록 분리한다.
		liveEvidence.currentProviderStreams = cloneCurrentProviderStreams(streamsByChannel)
	}

	// 외부 요청 대기 중 도착한 정상 관측을 미래 시각으로 오인하지 않도록 조회 직전에 경계를 잡는다.
	persistedSessions, persistedErr := c.loadPersistedLiveSessions(ctx, dueChannels, time.Now().UTC())
	if persistedErr != nil {
		c.logPersistedLiveSourceError(persistedErr)
	}

	if c.shouldFailAfterHolodexError(holodexErr, persistedErr, persistedSessions) {
		return nil, nil, youtubeLiveCheckEvidence{}, nil, fmt.Errorf("check youtube streams: %w", holodexErr)
	}

	if holodexErr != nil {
		// Holodex 실패 뒤 persisted 세션만으로 이번 주기를 계속한 횟수다. 예외 계약(contracts/alarm.md)의 telemetry다.
		observeYouTubePersistedLiveSessions("holodex_error_continued", "all", 1)
	}

	liveEvidence.observedAtByStreamID = mergePersistedLiveSessionStreams(streamsByChannel, persistedSessions)

	err = c.applyConfirmedPremiereClassification(ctx, streamsByChannel)
	if err != nil {
		return nil, nil, liveEvidence, nil, fmt.Errorf("check youtube streams: classify confirmed premieres: %w", err)
	}

	subscriberMap, err = LoadSubscriberRoomsByChannel(ctx, c.cacheClient, c.subscribers, dueChannels)
	if err != nil {
		return nil, nil, liveEvidence, nil, fmt.Errorf("check youtube streams: load subscriber rooms: %w", err)
	}

	evidence := c.observePersistedLiveGuardrails(ctx, persistedSessions, streamsByChannel, subscriberMap, now)

	liveEvidence.sentRoomsByStreamID = evidence.sentRoomsByStreamID

	return dueChannels, streamsByChannel, liveEvidence, subscriberMap, nil
}

func (c *YouTubeChecker) selectDueYouTubeChannels(ctx context.Context, channelIDs []string, now time.Time) []string {
	dueChannels := c.tierScheduler.SelectDueChannels(channelIDs)

	persistedLiveChannels, err := c.loadPersistedLiveChannelIDs(ctx, channelIDs, now)
	if err != nil {
		c.logPersistedLiveSourceError(err)

		return dueChannels
	}

	return mergeSortedUniqueStrings(dueChannels, persistedLiveChannels)
}

func (c *YouTubeChecker) loadPersistedLiveChannelIDs(
	ctx context.Context,
	channelIDs []string,
	now time.Time,
) ([]string, error) {
	if c.persistedLiveSource == nil {
		return nil, nil
	}

	channels, err := c.persistedLiveSource.LoadRecentLiveChannelIDs(ctx, channelIDs, now)
	if err != nil {
		observeYouTubePersistedLiveSessions("channel_load_error", "live", 1)

		return nil, fmt.Errorf("load persisted live channel ids: %w", err)
	}

	if len(channels) > 0 {
		observeYouTubePersistedLiveSessions("channel_due_forced", "live", len(channels))
	}

	return channels, nil
}

func mergeSortedUniqueStrings(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	result := make([]string, 0, len(a)+len(b))

	for _, value := range append(a, b...) {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}

		if _, ok := seen[value]; ok {
			continue
		}

		seen[value] = struct{}{}
		result = append(result, value)
	}

	slices.Sort(result)

	return result
}

func (c *YouTubeChecker) loadHolodexStreamsByChannel(
	ctx context.Context,
	dueChannels []string,
) (map[string][]*domain.Stream, error) {
	holodexCtx, cancel := context.WithTimeout(ctx, c.holodexBudget)
	defer cancel()

	streams, err := c.holodexService.GetChannelsLiveStatus(holodexCtx, dueChannels)
	if err == nil {
		return groupStreamsByChannel(streams), nil
	}

	if c.persistedLiveSource != nil {
		c.logger.Warn("YouTube Holodex live status source failed; continuing with persisted live sessions",
			slog.Any("error", err),
		)
	}

	return nil, fmt.Errorf("fetch channels live status: %w", err)
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
