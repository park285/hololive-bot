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
	stdErrors "errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/park285/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
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
// org/status/sort 필터 없이 live+upcoming을 모두 반환한다.
// 사용 시나리오: 알림 체크, 대시보드 상태 표시 등 빠른 상태 확인
func (h *Service) GetChannelsLiveStatus(ctx context.Context, channelIDs []string) ([]*domain.Stream, error) {
	streams, failed, err := h.GetChannelsLiveStatusWithFailures(ctx, channelIDs)
	if err != nil {
		return nil, err
	}
	if len(streams) == 0 && len(failed) > 0 {
		return nil, fmt.Errorf("get channels live status: %w", joinChannelsLiveStatusFailures(channelIDs, failed))
	}
	return streams, nil
}

// failed map은 fetch 실패 채널을 "방송 없음" 채널과 구분해 live session 오종료를 막는 계약이다.
func (h *Service) GetChannelsLiveStatusWithFailures(ctx context.Context, channelIDs []string) ([]*domain.Stream, map[string]error, error) {
	if len(channelIDs) == 0 {
		return []*domain.Stream{}, nil, nil
	}

	if cached, found := h.cacheManager.GetChannelsLiveStatusStreams(ctx, channelIDs); found {
		return cached, nil, nil
	}

	params := url.Values{}
	params.Set("channels", strings.Join(channelIDs, ","))

	body, err := h.requester.DoRequest(ctx, "GET", "/users/live", params)
	if err != nil {
		return h.handleChannelsLiveStatusRequestError(ctx, channelIDs, err)
	}

	streams, mapErr := h.mapAndCacheChannelsLiveStatus(ctx, channelIDs, body)
	if mapErr != nil {
		return nil, nil, mapErr
	}
	return streams, nil, nil
}

func (h *Service) handleChannelsLiveStatusRequestError(ctx context.Context, channelIDs []string, err error) ([]*domain.Stream, map[string]error, error) {
	h.logger.Error("Failed to get channels live status",
		slog.Int("channel_count", len(channelIDs)),
		slog.Any("error", err),
	)

	allStreams, failed, fallbackErr, ok := h.tryChannelsLiveStatusFallback(ctx, channelIDs, err)
	if ok {
		return allStreams, failed, nil
	}
	if fallbackErr != nil {
		return nil, nil, fmt.Errorf("get channels live status: %w", stdErrors.Join(err, fallbackErr))
	}
	return nil, nil, fmt.Errorf("get channels live status: %w", err)
}

func (h *Service) tryChannelsLiveStatusFallback(ctx context.Context, channelIDs []string, err error) ([]*domain.Stream, map[string]error, error, bool) {
	if !h.shouldUseFallback(ctx, err) || h.scraper == nil {
		return nil, nil, nil, false
	}

	h.logger.Warn("Using scraper fallback for channels live status", slog.Any("error", err))
	allStreams, failed := h.getChannelsLiveStatusFromScraper(ctx, channelIDs)
	h.logChannelsLiveStatusFallbackFailures(channelIDs, failed)
	return h.resolveChannelsLiveStatusFallback(ctx, channelIDs, allStreams, failed)
}

func (h *Service) logChannelsLiveStatusFallbackFailures(channelIDs []string, failed map[string]error) {
	if len(failed) == 0 {
		return
	}
	h.logger.Warn("Scraper live status fallback failed for some channels",
		slog.Int("channel_count", len(channelIDs)),
		slog.Int("failed_count", len(failed)),
	)
}

func (h *Service) resolveChannelsLiveStatusFallback(ctx context.Context, channelIDs []string, allStreams []*domain.Stream, failed map[string]error) ([]*domain.Stream, map[string]error, error, bool) {
	if sourceLevelErr := firstChannelsLiveStatusSourceLevelError(channelIDs, failed); sourceLevelErr != nil {
		return nil, nil, fmt.Errorf("fetch channels live status from scraper: %w", sourceLevelErr), false
	}
	if len(failed) > 0 && len(failed) == len(channelIDs) {
		return nil, nil, fmt.Errorf("fetch channels live status from scraper: %w", joinChannelsLiveStatusFailures(channelIDs, failed)), false
	}
	if len(failed) > 0 {
		return allStreams, failed, nil, true
	}
	if len(allStreams) == 0 {
		return nil, nil, nil, false
	}

	h.cacheManager.SetChannelsLiveStatusStreams(ctx, channelIDs, allStreams, 30*time.Second)
	return allStreams, nil, nil, true
}

func firstChannelsLiveStatusSourceLevelError(channelIDs []string, failed map[string]error) error {
	for _, channelID := range channelIDs {
		if channelErr, ok := failed[channelID]; ok && isYouTubeProducerSourceLevelFallbackError(channelErr) {
			return channelErr
		}
	}
	return nil
}

func joinChannelsLiveStatusFailures(channelIDs []string, failed map[string]error) error {
	errs := make([]error, 0, len(failed))
	for _, channelID := range channelIDs {
		if channelErr, ok := failed[channelID]; ok {
			errs = append(errs, fmt.Errorf("channel %s: %w", channelID, channelErr))
		}
	}
	return stdErrors.Join(errs...)
}

func isYouTubeProducerSourceLevelFallbackError(err error) bool {
	return stdErrors.Is(err, scraper.ErrRateLimited) ||
		stdErrors.Is(err, scraper.ErrForbidden) ||
		stdErrors.Is(err, scraper.ErrBlockedResponse)
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
	if override, ok := constants.IndieChannelOrgOverrides[stream.ChannelID]; ok {
		org := override
		stream.Channel.Org = &org
		return
	}
	if stream.Channel.Org == nil || *stream.Channel.Org == "" {
		stream.Channel.Org = &indie
	}
}

func (h *Service) getChannelsLiveStatusFromScraper(ctx context.Context, channelIDs []string) ([]*domain.Stream, map[string]error) {
	allStreams := make([]*domain.Stream, 0, len(channelIDs))
	var failed map[string]error

	for _, channelID := range channelIDs {
		streams, err := h.scraper.FetchFromYouTubeProducer(ctx, channelID)
		if err != nil {
			if failed == nil {
				failed = make(map[string]error, 1)
			}
			failed[channelID] = err
			continue
		}
		allStreams = append(allStreams, streams...)
	}

	return allStreams, failed
}
