// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package holodexprovider

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/park285/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type channelFetchResult struct {
	id      string
	channel *domain.Channel
}

func (h *Service) GetChannels(ctx context.Context, channelIDs []string) (map[string]*domain.Channel, error) {
	if len(channelIDs) == 0 {
		return make(map[string]*domain.Channel), nil
	}

	result, missedIDs := h.collectCachedChannels(ctx, channelIDs)
	h.logGetChannelsCacheStatus(channelIDs, result, missedIDs)

	if len(missedIDs) == 0 {
		return result, nil
	}

	allChannels, err := h.fetchHololiveChannelList(ctx)
	if err != nil {
		return h.handleChannelListFetchError(ctx, channelIDs, result, missedIDs, err)
	}

	h.addMissedChannelsFromList(ctx, result, missedIDs, allChannels)

	h.logger.Info("GetChannels batch complete (optimized)",
		slog.Int("requested", len(channelIDs)),
		slog.Int("returned", len(result)),
		slog.Int("from_list_api", len(result)-len(channelIDs)+len(missedIDs)),
	)

	return result, nil
}

func (h *Service) collectCachedChannels(ctx context.Context, channelIDs []string) (map[string]*domain.Channel, []string) {
	result := make(map[string]*domain.Channel, len(channelIDs))
	var missedIDs []string

	for _, id := range channelIDs {
		if cached, found := h.cacheManager.GetChannel(ctx, id); found {
			result[id] = cached
			continue
		}
		missedIDs = append(missedIDs, id)
	}

	return result, missedIDs
}

func (h *Service) logGetChannelsCacheStatus(channelIDs []string, result map[string]*domain.Channel, missedIDs []string) {
	h.logger.Debug("GetChannels cache status",
		slog.Int("total", len(channelIDs)),
		slog.Int("cache_hits", len(result)),
		slog.Int("cache_misses", len(missedIDs)),
	)
}

func (h *Service) handleChannelListFetchError(ctx context.Context, channelIDs []string, result map[string]*domain.Channel, missedIDs []string, err error) (map[string]*domain.Channel, error) {
	if !h.shouldUseFallback(ctx, err) {
		return result, fmt.Errorf("get channels batch list: %w", err)
	}

	h.logger.Warn("Failed to fetch channel list, falling back to individual queries",
		slog.Any("error", err),
		slog.Int("missed_count", len(missedIDs)),
	)
	return h.fetchChannelsIndividually(ctx, channelIDs, result, missedIDs)
}

func (h *Service) addMissedChannelsFromList(ctx context.Context, result map[string]*domain.Channel, missedIDs []string, allChannels []*domain.Channel) {
	missedSet := stringSet(missedIDs)
	for _, ch := range allChannels {
		if missedSet[ch.ID] {
			result[ch.ID] = ch
			h.cacheManager.SetChannel(ctx, ch.ID, ch)
		}
	}
}

func stringSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

// /users/live 엔드포인트를 우선 사용하고, retryable 오류에서만 채널별 YouTube producer 경로로 제한 폴백합니다.
// 이 경로는 공식 스케줄 페이지 재조회 없이 YouTube producer 결과만 사용합니다.
// 주의: org, status, sort 필터링 미지원 - live+upcoming 모두 반환됨
// 사용 시나리오: 알림 체크, 대시보드 상태 표시 등 빠른 상태 확인
func (h *Service) GetChannelsLiveStatus(ctx context.Context, channelIDs []string) ([]*domain.Stream, error) {
	if len(channelIDs) == 0 {
		return []*domain.Stream{}, nil
	}

	if cached, found := h.cacheManager.GetChannelsLiveStatusStreams(ctx, channelIDs); found {
		return cached, nil
	}

	params := url.Values{}
	params.Set("channels", strings.Join(channelIDs, ","))

	body, err := h.requester.DoRequest(ctx, "GET", "/users/live", params)
	if err != nil {
		return h.handleChannelsLiveStatusRequestError(ctx, channelIDs, err)
	}

	return h.mapAndCacheChannelsLiveStatus(ctx, channelIDs, body)
}

func (h *Service) handleChannelsLiveStatusRequestError(ctx context.Context, channelIDs []string, err error) ([]*domain.Stream, error) {
	h.logger.Error("Failed to get channels live status",
		slog.Int("channel_count", len(channelIDs)),
		slog.Any("error", err),
	)

	allStreams, ok := h.tryChannelsLiveStatusFallback(ctx, channelIDs, err)
	if ok {
		return allStreams, nil
	}
	return nil, fmt.Errorf("get channels live status: %w", err)
}

func (h *Service) tryChannelsLiveStatusFallback(ctx context.Context, channelIDs []string, err error) ([]*domain.Stream, bool) {
	if !h.shouldUseFallback(ctx, err) || h.scraper == nil {
		return nil, false
	}

	h.logger.Warn("Using scraper fallback for channels live status", slog.Any("error", err))
	allStreams, fallbackErr := h.getChannelsLiveStatusFromScraper(ctx, channelIDs)
	if fallbackErr != nil {
		h.logger.Warn("Scraper live status fallback failed",
			slog.Int("channel_count", len(channelIDs)),
			slog.Any("error", fallbackErr),
		)
	}
	if len(allStreams) == 0 {
		return nil, false
	}

	h.cacheManager.SetChannelsLiveStatusStreams(ctx, channelIDs, allStreams, 30*time.Second)
	return allStreams, true
}

func (h *Service) mapAndCacheChannelsLiveStatus(ctx context.Context, channelIDs []string, body []byte) ([]*domain.Stream, error) {
	var rawStreams []StreamRaw
	if err := json.Unmarshal(body, &rawStreams); err != nil {
		return nil, fmt.Errorf("failed to unmarshal channels live status: %w", err)
	}

	streams := h.mapper.MapStreamsResponse(rawStreams)
	h.hydrateIndieStreamChannels(streams, channelIDs)
	filtered := h.filter.FilterHololiveStreams(streams)

	h.cacheManager.SetChannelsLiveStatusStreams(ctx, channelIDs, filtered, 30*time.Second)
	h.logger.Debug("GetChannelsLiveStatus completed",
		slog.Int("requested_channels", len(channelIDs)),
		slog.Int("streams_found", len(filtered)),
	)

	return filtered, nil
}

func (h *Service) hydrateIndieStreamChannels(streams []*domain.Stream, requestedChannelIDs []string) {
	indieRequested := requestedIndieChannels(requestedChannelIDs)
	if len(streams) == 0 || len(indieRequested) == 0 {
		return
	}

	h.applyIndieStreamChannels(streams, indieRequested)
}

func requestedIndieChannels(requestedChannelIDs []string) map[string]struct{} {
	if len(requestedChannelIDs) == 0 || len(constants.IndieChannelIDs) == 0 {
		return nil
	}

	indieRequested := make(map[string]struct{}, len(constants.IndieChannelIDs))
	for _, channelID := range requestedChannelIDs {
		if channelID == "" {
			continue
		}
		if slices.Contains(constants.IndieChannelIDs, channelID) {
			indieRequested[channelID] = struct{}{}
		}
	}
	return indieRequested
}

func (h *Service) applyIndieStreamChannels(streams []*domain.Stream, indieRequested map[string]struct{}) {
	indie := constants.HolodexAPIParams.OrgIndie
	for _, stream := range streams {
		h.hydrateIndieStreamChannel(stream, indieRequested, indie)
	}
}

func (h *Service) hydrateIndieStreamChannel(stream *domain.Stream, indieRequested map[string]struct{}, indie string) {
	if stream == nil || stream.ChannelID == "" {
		return
	}
	if _, ok := indieRequested[stream.ChannelID]; !ok {
		return
	}

	if stream.Channel == nil {
		stream.Channel = &domain.Channel{
			ID:   stream.ChannelID,
			Name: stream.ChannelName,
		}
	}
	if stream.Channel.Org == nil || *stream.Channel.Org == "" {
		stream.Channel.Org = &indie
	}
}

func (h *Service) getChannelsLiveStatusFromScraper(ctx context.Context, channelIDs []string) ([]*domain.Stream, error) {
	allStreams := make([]*domain.Stream, 0, len(channelIDs))
	var lastErr error

	for _, channelID := range channelIDs {
		streams, err := h.scraper.FetchFromYouTubeProducer(ctx, channelID)
		if err != nil {
			lastErr = err
			continue
		}
		allStreams = append(allStreams, streams...)
	}

	if lastErr != nil {
		return allStreams, fmt.Errorf("fetch channels live status from scraper: %w", lastErr)
	}

	return allStreams, nil
}
