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

package holodex

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"

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

// /users/live 엔드포인트를 우선 사용하고, retryable 오류에서만 채널별 YouTube scraper 경로로 제한 폴백합니다.
// 이 경로는 공식 스케줄 페이지 재조회 없이 YouTube scraper 결과만 사용합니다.
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
		streams, err := h.scraper.fetchFromYouTubeScraper(ctx, channelID)
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

// fetchHololiveChannelList: Hololive 채널 목록을 /channels API로 조회합니다.
// 내부 캐시를 사용하여 반복 호출 시 효율을 높입니다.
// Holodex API limit=50 제한으로 인해 페이지네이션을 사용합니다.
func (h *Service) fetchHololiveChannelList(ctx context.Context) ([]*domain.Channel, error) {
	if cached, found := h.cacheManager.GetHololiveChannelList(ctx); found {
		return cached, nil
	}

	allChannels, err := h.fetchHololiveChannelListPages(ctx)
	if err != nil {
		return nil, err
	}

	h.logger.Debug("Fetched all Hololive channels", slog.Int("total", len(allChannels)))
	h.cacheManager.SetHololiveChannelList(ctx, allChannels, 5*time.Minute)

	return allChannels, nil
}

func (h *Service) fetchHololiveChannelListPages(ctx context.Context) ([]*domain.Channel, error) {
	var allChannels []*domain.Channel
	pageSize := constants.HolodexAPIParams.DefaultChannelLimit
	offset := 0
	for {
		channels, rawCount, err := h.fetchHololiveChannelListPage(ctx, pageSize, offset)
		if err != nil {
			return nil, err
		}

		allChannels = append(allChannels, channels...)
		if rawCount < pageSize {
			break
		}

		offset += pageSize
		if h.channelListPaginationLimitReached(offset) {
			break
		}
	}

	return allChannels, nil
}

func (h *Service) fetchHololiveChannelListPage(ctx context.Context, pageSize int, offset int) ([]*domain.Channel, int, error) {
	params := url.Values{}
	params.Set("org", constants.HolodexAPIParams.OrgHololive)
	params.Set("type", constants.HolodexAPIParams.TypeVtuber)
	params.Set("limit", fmt.Sprintf("%d", pageSize))
	params.Set("offset", fmt.Sprintf("%d", offset))

	body, err := h.requester.DoRequest(ctx, "GET", "/channels", params)
	if err != nil {
		return nil, 0, fmt.Errorf("fetch hololive channel list (offset=%d): %w", offset, err)
	}

	var rawChannels []ChannelRaw
	if err := json.Unmarshal(body, &rawChannels); err != nil {
		return nil, 0, fmt.Errorf("failed to unmarshal channel list: %w", err)
	}

	return h.mapper.MapChannelsResponse(rawChannels), len(rawChannels), nil
}

func (h *Service) channelListPaginationLimitReached(offset int) bool {
	if offset < constants.HolodexAPIParams.MaxPaginationOffset {
		return false
	}
	h.logger.Warn("Pagination limit reached", slog.Int("offset", offset))
	return true
}

// fetchChannelsIndividually: 개별 /channels/{id} API로 채널을 조회합니다. (폴백용)
func (h *Service) fetchChannelsIndividually(ctx context.Context, channelIDs []string, result map[string]*domain.Channel, missedIDs []string) (map[string]*domain.Channel, error) {
	const maxConcurrent = 5
	if len(missedIDs) == 0 {
		return result, nil
	}

	workerCount := min(maxConcurrent, len(missedIDs))
	jobs := make(chan string)
	resultChan := make(chan channelFetchResult, len(missedIDs))
	workerWG := h.startChannelFetchWorkers(ctx, workerCount, jobs, resultChan)

	go func() {
		enqueueChannelFetchJobs(ctx, jobs, missedIDs)
	}()

	go func() {
		workerWG.Wait()
		close(resultChan)
	}()

	return h.collectIndividualChannelFetchResults(ctx, channelIDs, result, resultChan)
}

func (h *Service) startChannelFetchWorkers(ctx context.Context, workerCount int, jobs <-chan string, resultChan chan<- channelFetchResult) *sync.WaitGroup {
	workerWG := &sync.WaitGroup{}
	workerWG.Add(workerCount)
	for range workerCount {
		go h.runChannelFetchWorker(ctx, jobs, resultChan, workerWG)
	}
	return workerWG
}

func (h *Service) runChannelFetchWorker(ctx context.Context, jobs <-chan string, resultChan chan<- channelFetchResult, workerWG *sync.WaitGroup) {
	defer workerWG.Done()
	for channelID := range jobs {
		if ctx.Err() != nil {
			resultChan <- channelFetchResult{id: channelID}
			continue
		}
		resultChan <- h.fetchIndividualChannel(ctx, channelID)
	}
}

func (h *Service) fetchIndividualChannel(ctx context.Context, channelID string) channelFetchResult {
	channel, err := h.fetchChannelDirect(ctx, channelID)
	if err != nil {
		h.logger.Warn("Failed to get channel in batch",
			slog.String("channel_id", channelID),
			slog.Any("error", err),
		)
		return channelFetchResult{id: channelID}
	}
	return channelFetchResult{id: channelID, channel: channel}
}

func enqueueChannelFetchJobs(ctx context.Context, jobs chan<- string, missedIDs []string) {
	defer close(jobs)
	for _, id := range missedIDs {
		if ctx.Err() != nil {
			return
		}
		jobs <- id
	}
}

func (h *Service) collectIndividualChannelFetchResults(ctx context.Context, channelIDs []string, result map[string]*domain.Channel, resultChan <-chan channelFetchResult) (map[string]*domain.Channel, error) {
	for r := range resultChan {
		if r.channel != nil {
			result[r.id] = r.channel
		}
	}
	if ctx.Err() != nil {
		return result, fmt.Errorf("batch channel fetch canceled: %w", ctx.Err())
	}
	h.logger.Info("GetChannels batch complete (fallback)",
		slog.Int("requested", len(channelIDs)),
		slog.Int("returned", len(result)),
	)
	return result, nil
}
