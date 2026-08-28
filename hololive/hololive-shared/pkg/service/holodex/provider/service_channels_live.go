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
	"maps"
	"net/http"
	"net/url"
	"strings"
	"time"

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
		fallbackResult, fallbackErr := h.handleChannelListFetchError(ctx, channelIDs, result, missedIDs, err)
		if fallbackErr != nil {
			return nil, fmt.Errorf("handle channel list fetch error: %w", fallbackErr)
		}

		return fallbackResult, nil
	}

	h.addMissedChannelsFromList(ctx, result, missedIDs, allChannels)

	h.logger.Info("GetChannels batch complete (optimized)",
		slog.Int("requested", len(channelIDs)),
		slog.Int("returned", len(result)),
		slog.Int("from_list_api", len(result)-len(channelIDs)+len(missedIDs)),
	)

	return result, nil
}

func (h *Service) collectCachedChannels(ctx context.Context, channelIDs []string) (result0 map[string]*domain.Channel, result1 []string) {
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
		return nil, fmt.Errorf("get channels batch list: %w", err)
	}

	h.logger.Warn("Failed to fetch channel list, falling back to individual queries",
		slog.Any("error", err),
		slog.Int("missed_count", len(missedIDs)),
	)

	fetched, fetchErr := h.fetchChannelsIndividually(ctx, channelIDs, result, missedIDs)
	if fetchErr != nil {
		return nil, fmt.Errorf("fetch channels individually: %w", fetchErr)
	}

	return fetched, nil
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
// 이 경로는 org/status/sort 필터 없이 live+upcoming을 모두 반환한다.
// 사용 시나리오: 알림 체크, 대시보드 상태 표시 등 빠른 상태 확인.
func (h *Service) GetChannelsLiveStatus(ctx context.Context, channelIDs []string) ([]*domain.Stream, error) {
	streams, failed, err := h.GetChannelsLiveStatusWithFailures(ctx, channelIDs)
	if err != nil {
		return nil, fmt.Errorf("get channels live status with failures: %w", err)
	}

	if len(failed) > 0 {
		return streams, fmt.Errorf("get channels live status: %w", joinChannelsLiveStatusFailures(channelIDs, failed))
	}

	return streams, nil
}

// failures map은 fetch 실패 채널을 "방송 없음" 채널과 구분해 live session 오종료를 막는 계약이다.
type channelsLiveStatus struct {
	streams  []*domain.Stream
	failures map[string]error
}

func (h *Service) GetChannelsLiveStatusWithFailures(ctx context.Context, channelIDs []string) ([]*domain.Stream, map[string]error, error) {
	status, err := h.fetchChannelsLiveStatus(ctx, channelIDs)
	if err != nil {
		return nil, nil, fmt.Errorf("channels live status: %w", err)
	}

	return status.streams, status.failures, nil
}

func (h *Service) fetchChannelsLiveStatus(ctx context.Context, channelIDs []string) (channelsLiveStatus, error) {
	if len(channelIDs) == 0 {
		return channelsLiveStatus{streams: []*domain.Stream{}}, nil
	}

	if cached, found := h.cacheManager.GetChannelsLiveStatusStreams(ctx, channelIDs); found {
		return channelsLiveStatus{streams: cached}, nil
	}

	params := url.Values{}
	params.Set("channels", strings.Join(channelIDs, ","))

	body, err := h.requester.DoRequest(ctx, http.MethodGet, usersLivePath, params)
	if err != nil {
		status, handleErr := h.handleChannelsLiveStatusRequestError(ctx, channelIDs, err)
		if handleErr != nil {
			return channelsLiveStatus{}, fmt.Errorf("handle channels live status request error: %w", handleErr)
		}

		return status, nil
	}

	streams, mapErr := h.mapAndCacheChannelsLiveStatus(ctx, channelIDs, body)
	if mapErr != nil {
		return channelsLiveStatus{}, fmt.Errorf("map and cache channels live status: %w", mapErr)
	}

	return channelsLiveStatus{streams: streams}, nil
}

func (h *Service) handleChannelsLiveStatusRequestError(ctx context.Context, channelIDs []string, err error) (channelsLiveStatus, error) {
	h.logger.Error("Failed to get channels live status",
		slog.Int("channel_count", len(channelIDs)),
		slog.Any("error", err),
	)

	status, resolved, fallbackErr := h.tryChannelsLiveStatusFallback(ctx, channelIDs, err)
	if resolved {
		return status, nil
	}

	if fallbackErr != nil {
		return channelsLiveStatus{}, fmt.Errorf("get channels live status: %w", stdErrors.Join(err, fallbackErr))
	}

	return channelsLiveStatus{}, fmt.Errorf("get channels live status: %w", err)
}

func (h *Service) tryChannelsLiveStatusFallback(ctx context.Context, channelIDs []string, err error) (channelsLiveStatus, bool, error) {
	if !h.shouldUseFallback(ctx, err) || h.scraper == nil {
		return channelsLiveStatus{}, false, nil
	}

	h.logger.Warn("Using scraper fallback for channels live status", slog.Any("error", err))

	result := h.getChannelsLiveStatusFromScraper(ctx, channelIDs)
	h.logChannelsLiveStatusFallbackFailures(channelIDs, result.failed, result.deferred)

	status, resolved, resolveErr := h.resolveChannelsLiveStatusFallback(ctx, channelIDs, result)
	if resolveErr != nil {
		return channelsLiveStatus{}, false, fmt.Errorf("resolve channels live status fallback: %w", resolveErr)
	}

	return status, resolved, nil
}

func (h *Service) logChannelsLiveStatusFallbackFailures(channelIDs []string, failed, deferred map[string]error) {
	if len(failed) > 0 {
		h.logger.Warn("Scraper live status fallback failed for some channels",
			slog.Int("channel_count", len(channelIDs)),
			slog.Int("failed_count", len(failed)),
		)
	}

	if len(deferred) > 0 {
		h.logger.Debug("Scraper live status fallback deferred for some channels",
			slog.Int("channel_count", len(channelIDs)),
			slog.Int("deferred_count", len(deferred)),
		)
	}
}

func (h *Service) resolveChannelsLiveStatusFallback(ctx context.Context, channelIDs []string, result channelsLiveStatusFallbackResult) (channelsLiveStatus, bool, error) {
	if sourceLevelErr := firstChannelsLiveStatusSourceLevelError(channelIDs, result.failed); sourceLevelErr != nil {
		return channelsLiveStatus{}, false, fmt.Errorf("fetch channels live status from scraper: %w", sourceLevelErr)
	}

	if len(result.failed) > 0 && len(result.failed) == len(channelIDs) {
		return channelsLiveStatus{}, false, fmt.Errorf("fetch channels live status from scraper: %w", joinChannelsLiveStatusFailures(channelIDs, result.failed))
	}

	unresolved := mergeChannelsLiveStatusFailures(result.failed, result.deferred)
	if len(unresolved) > 0 {
		return channelsLiveStatus{streams: result.streams, failures: unresolved}, true, nil
	}

	if len(result.streams) == 0 {
		return channelsLiveStatus{}, false, nil
	}

	h.cacheManager.SetChannelsLiveStatusStreams(ctx, channelIDs, result.streams, 30*time.Second)

	return channelsLiveStatus{streams: result.streams}, true, nil
}

func mergeChannelsLiveStatusFailures(failed, deferred map[string]error) map[string]error {
	if len(failed) == 0 && len(deferred) == 0 {
		return nil
	}

	merged := make(map[string]error, len(failed)+len(deferred))
	maps.Copy(merged, deferred)
	maps.Copy(merged, failed)

	return merged
}

func firstChannelsLiveStatusSourceLevelError(channelIDs []string, failed map[string]error) error {
	for _, channelID := range channelIDs {
		if channelErr, ok := failed[channelID]; ok && isYouTubeScraperSourceLevelFallbackError(channelErr) {
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
