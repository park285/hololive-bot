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
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// GetChannelSchedule: 특정 채널의 방송 일정(예정된 방송)을 조회합니다.
// includeLive가 true이면 현재 진행 중인 방송도 포함한다.
func (h *Service) GetChannelSchedule(ctx context.Context, channelID string, hours int, includeLive bool) ([]*domain.Stream, error) {
	if cached, found := h.cacheManager.GetChannelSchedule(ctx, channelID, hours, includeLive); found {
		copied := make([]*domain.Stream, len(cached))
		for i, stream := range cached {
			streamCopy := *stream
			if stream.StartScheduled != nil {
				t := *stream.StartScheduled
				streamCopy.StartScheduled = &t
			}
			if stream.StartActual != nil {
				t := *stream.StartActual
				streamCopy.StartActual = &t
			}
			copied[i] = &streamCopy
		}

		if includeLive {
			return copied, nil
		}
		return h.filter.FilterUpcomingStreams(copied), nil
	}

	var statusStr string
	if includeLive {
		statusStr = string(domain.StreamStatusLive) + "," + string(domain.StreamStatusUpcoming)
	} else {
		statusStr = string(domain.StreamStatusUpcoming)
	}

	params := url.Values{}
	params.Set("channel_id", channelID)
	params.Set("status", statusStr)
	params.Set("type", "stream")
	params.Set("max_upcoming_hours", fmt.Sprintf("%d", hours))

	body, err := h.requester.DoRequest(ctx, "GET", "/live", params)
	if err != nil {
		h.logger.Error("Failed to get channel schedule",
			slog.String("channel_id", channelID),
			slog.String("status", statusStr),
			slog.Any("error", err),
		)

		if h.shouldUseFallback(ctx, err) && h.scraper != nil {
			h.logger.Warn("Using scraper fallback for channel schedule",
				slog.String("channel_id", channelID),
				slog.Any("error", err))

			return h.scraper.FetchChannel(ctx, channelID)
		}

		return nil, fmt.Errorf("get channel schedule: %w", err)
	}

	var rawStreams []StreamRaw
	if err := json.Unmarshal(body, &rawStreams); err != nil {
		return nil, fmt.Errorf("failed to unmarshal channel schedule: %w", err)
	}

	allStreams := h.mapper.MapStreamsResponse(rawStreams)
	hololiveOnly := h.filter.FilterHololiveStreams(allStreams)

	slices.SortFunc(hololiveOnly, func(a, b *domain.Stream) int {
		aTime := int64(0)
		if a.StartScheduled != nil {
			aTime = a.StartScheduled.Unix()
		}
		bTime := int64(0)
		if b.StartScheduled != nil {
			bTime = b.StartScheduled.Unix()
		}
		return cmp.Compare(aTime, bTime)
	})

	result := hololiveOnly
	if !includeLive {
		result = h.filter.FilterUpcomingStreams(hololiveOnly)
	}

	h.cacheManager.SetChannelSchedule(ctx, channelID, hours, includeLive, result, constants.CacheTTL.ChannelSchedule)

	return result, nil
}

// SearchChannels: 채널 이름 검색 쿼리를 통해 해당하는 Hololive 채널 목록을 조회합니다.
func (h *Service) SearchChannels(ctx context.Context, query string) ([]*domain.Channel, error) {
	if cached, found := h.cacheManager.GetSearchChannels(ctx, query); found {
		return cached, nil
	}

	query = stringutil.TrimSpace(query)
	params := url.Values{}
	params.Set("org", constants.HolodexAPIParams.OrgHololive)
	params.Set("type", constants.HolodexAPIParams.TypeVtuber)
	params.Set("limit", fmt.Sprintf("%d", constants.HolodexAPIParams.DefaultChannelLimit))

	body, err := h.requester.DoRequest(ctx, "GET", "/channels", params)
	if err != nil {
		h.logger.Error("Failed to search channels", slog.String("query", query), slog.Any("error", err))
		return nil, fmt.Errorf("search channels: %w", err)
	}

	var rawChannels []ChannelRaw
	if err := json.Unmarshal(body, &rawChannels); err != nil {
		return nil, fmt.Errorf("failed to unmarshal search channels: %w", err)
	}

	channels := h.mapper.MapChannelsResponse(rawChannels)

	h.logger.Debug("Holodex API search results",
		slog.String("query", query),
		slog.Int("total_results", len(channels)),
	)

	filtered := make([]*domain.Channel, 0, len(channels))
	normalizedQuery := strings.ToLower(query)
	for _, ch := range channels {
		if ch.Org != nil && *ch.Org == "Hololive" && !h.filter.IsHolostarsChannel(ch) {
			if normalizedQuery == "" {
				filtered = append(filtered, ch)
				continue
			}
			nameMatch := strings.Contains(strings.ToLower(ch.Name), normalizedQuery)
			englishMatch := ch.EnglishName != nil && strings.Contains(strings.ToLower(*ch.EnglishName), normalizedQuery)
			if nameMatch || englishMatch {
				filtered = append(filtered, ch)
			}
		}
	}

	h.logger.Debug("After HOLOSTARS filter", slog.Int("count", len(filtered)))

	h.cacheManager.SetSearchChannels(ctx, query, filtered)

	return filtered, nil
}

func buildSearchChannelsCacheKey(query string) string {
	normalized := stringutil.Normalize(query)
	if normalized == "" {
		return searchChannelsCacheKeyPrefix + "empty"
	}

	sum := sha256.Sum256([]byte(normalized))
	return searchChannelsCacheKeyPrefix + hex.EncodeToString(sum[:])
}

// GetChannel: 채널 ID로 특정 채널의 상세 정보를 조회합니다.
// retryable Holodex 오류(5xx/timeout/circuit/key rotation)에서만 YouTube 스크래퍼로 폴백하고,
// non-retryable 오류는 그대로 반환합니다.
func (h *Service) GetChannel(ctx context.Context, channelID string) (*domain.Channel, error) {
	if cached, found := h.cacheManager.GetChannel(ctx, channelID); found {
		return cached, nil
	}

	channel, err := h.fetchChannelDirect(ctx, channelID)
	if err == nil {
		return channel, nil
	}

	if h.shouldUseFallback(ctx, err) {
		h.logger.Warn("Using scraper fallback for channel",
			slog.String("channel_id", channelID),
			slog.Any("error", err),
		)

		channel, fallbackErr := h.getChannelFromScraper(ctx, channelID)
		if fallbackErr == nil {
			return channel, nil
		}

		return nil, fmt.Errorf(
			"get channel: primary and scraper fallback failed: %w",
			errorsJoin(err, fallbackErr),
		)
	}

	h.logger.Error("Failed to get channel", slog.String("channel_id", channelID), slog.Any("error", err))
	return nil, fmt.Errorf("get channel: %w", err)
}

func (h *Service) fetchChannelDirect(ctx context.Context, channelID string) (*domain.Channel, error) {
	body, err := h.requester.DoRequest(ctx, "GET", "/channels/"+channelID, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch channel direct: %w", err)
	}

	var rawChannel ChannelRaw
	if err := json.Unmarshal(body, &rawChannel); err != nil {
		return nil, fmt.Errorf("failed to unmarshal channel: %w", err)
	}

	channel := h.mapper.MapChannelResponse(&rawChannel)
	h.cacheManager.SetChannel(ctx, channelID, channel)

	return channel, nil
}

// getChannelFromScraper: YouTube 스크래퍼를 사용하여 채널 정보를 조회합니다. (Holodex 폴백)
func (h *Service) getChannelFromScraper(ctx context.Context, channelID string) (*domain.Channel, error) {
	if h.scraper == nil {
		return nil, fmt.Errorf("scraper fallback not configured")
	}

	stats, err := h.scraper.GetChannelStats(ctx, channelID)
	if err != nil {
		h.logger.Warn("Scraper fallback also failed for channel",
			slog.String("channel", channelID),
			slog.Any("error", err))
		return nil, fmt.Errorf("get channel stats from scraper: %w", err)
	}

	subCount := int(stats.SubscriberCount)
	channel := &domain.Channel{
		ID:              channelID,
		SubscriberCount: &subCount,
	}

	snippet, snippetErr := h.scraper.GetChannelSnippet(ctx, channelID)
	if snippetErr == nil && snippet != nil {
		if len(snippet.Avatar) > 0 {
			channel.Photo = &snippet.Avatar[len(snippet.Avatar)-1].URL
		}
	}

	h.cacheManager.SetChannel(ctx, channelID, channel)

	h.logger.Info("Channel fetched via scraper fallback",
		slog.String("channel", channelID),
		slog.Int64("subscribers", stats.SubscriberCount))

	return channel, nil
}

// GetChannels: 여러 채널 ID로 채널 정보를 배치 조회합니다.
// 캐시를 우선 조회하고, 캐시 미스된 채널은 /channels 리스트 API로 한 번에 조회합니다.
// 기존 N+1 개별 호출 패턴을 단일 호출로 최적화하여 rate limit 부담을 대폭 감소시킵니다.
func (h *Service) GetChannels(ctx context.Context, channelIDs []string) (map[string]*domain.Channel, error) {
	if len(channelIDs) == 0 {
		return make(map[string]*domain.Channel), nil
	}

	result := make(map[string]*domain.Channel, len(channelIDs))
	var missedIDs []string

	for _, id := range channelIDs {
		if cached, found := h.cacheManager.GetChannel(ctx, id); found {
			result[id] = cached
		} else {
			missedIDs = append(missedIDs, id)
		}
	}

	h.logger.Debug("GetChannels cache status",
		slog.Int("total", len(channelIDs)),
		slog.Int("cache_hits", len(result)),
		slog.Int("cache_misses", len(missedIDs)),
	)

	if len(missedIDs) == 0 {
		return result, nil
	}

	allChannels, err := h.fetchHololiveChannelList(ctx)
	if err != nil {
		if !h.shouldUseFallback(ctx, err) {
			return result, fmt.Errorf("get channels batch list: %w", err)
		}

		h.logger.Warn("Failed to fetch channel list, falling back to individual queries",
			slog.Any("error", err),
			slog.Int("missed_count", len(missedIDs)),
		)
		return h.fetchChannelsIndividually(ctx, channelIDs, result, missedIDs)
	}

	missedSet := make(map[string]bool, len(missedIDs))
	for _, id := range missedIDs {
		missedSet[id] = true
	}

	for _, ch := range allChannels {
		if missedSet[ch.ID] {
			result[ch.ID] = ch
			h.cacheManager.SetChannel(ctx, ch.ID, ch)
		}
	}

	h.logger.Info("GetChannels batch complete (optimized)",
		slog.Int("requested", len(channelIDs)),
		slog.Int("returned", len(result)),
		slog.Int("from_list_api", len(result)-len(channelIDs)+len(missedIDs)),
	)

	return result, nil
}

// GetChannelsLiveStatus: 특정 채널들의 현재 생방송/예정 상태를 빠르게 조회합니다.
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
		h.logger.Error("Failed to get channels live status",
			slog.Int("channel_count", len(channelIDs)),
			slog.Any("error", err),
		)

		if h.shouldUseFallback(ctx, err) && h.scraper != nil {
			h.logger.Warn("Using scraper fallback for channels live status", slog.Any("error", err))
			allStreams, fallbackErr := h.getChannelsLiveStatusFromScraper(ctx, channelIDs)
			if fallbackErr != nil {
				h.logger.Warn("Scraper live status fallback failed",
					slog.Int("channel_count", len(channelIDs)),
					slog.Any("error", fallbackErr),
				)
			}
			if len(allStreams) > 0 {
				h.cacheManager.SetChannelsLiveStatusStreams(ctx, channelIDs, allStreams, 30*time.Second)
				return allStreams, nil
			}
		}

		return nil, fmt.Errorf("get channels live status: %w", err)
	}

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
	if len(streams) == 0 || len(requestedChannelIDs) == 0 || len(constants.IndieChannelIDs) == 0 {
		return
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
	if len(indieRequested) == 0 {
		return
	}

	indie := constants.HolodexAPIParams.OrgIndie
	for _, stream := range streams {
		if stream == nil || stream.ChannelID == "" {
			continue
		}
		if _, ok := indieRequested[stream.ChannelID]; !ok {
			continue
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

	var allChannels []*domain.Channel
	pageSize := constants.HolodexAPIParams.DefaultChannelLimit
	offset := 0

	for {
		params := url.Values{}
		params.Set("org", constants.HolodexAPIParams.OrgHololive)
		params.Set("type", constants.HolodexAPIParams.TypeVtuber)
		params.Set("limit", fmt.Sprintf("%d", pageSize))
		params.Set("offset", fmt.Sprintf("%d", offset))

		body, err := h.requester.DoRequest(ctx, "GET", "/channels", params)
		if err != nil {
			return nil, fmt.Errorf("fetch hololive channel list (offset=%d): %w", offset, err)
		}

		var rawChannels []ChannelRaw
		if err := json.Unmarshal(body, &rawChannels); err != nil {
			return nil, fmt.Errorf("failed to unmarshal channel list: %w", err)
		}

		channels := h.mapper.MapChannelsResponse(rawChannels)
		allChannels = append(allChannels, channels...)

		if len(rawChannels) < pageSize {
			break
		}

		offset += pageSize
		if offset >= constants.HolodexAPIParams.MaxPaginationOffset {
			h.logger.Warn("Pagination limit reached", slog.Int("offset", offset))
			break
		}
	}

	h.logger.Debug("Fetched all Hololive channels", slog.Int("total", len(allChannels)))
	h.cacheManager.SetHololiveChannelList(ctx, allChannels, 5*time.Minute)

	return allChannels, nil
}

// fetchChannelsIndividually: 개별 /channels/{id} API로 채널을 조회합니다. (폴백용)
func (h *Service) fetchChannelsIndividually(ctx context.Context, channelIDs []string, result map[string]*domain.Channel, missedIDs []string) (map[string]*domain.Channel, error) {
	const maxConcurrent = 5
	if len(missedIDs) == 0 {
		return result, nil
	}

	workerCount := min(maxConcurrent, len(missedIDs))

	jobs := make(chan string)
	resultChan := make(chan struct {
		id      string
		channel *domain.Channel
	}, len(missedIDs))

	var workerWG sync.WaitGroup
	worker := func() {
		defer workerWG.Done()
		for channelID := range jobs {
			select {
			case <-ctx.Done():
				resultChan <- struct {
					id      string
					channel *domain.Channel
				}{channelID, nil}
				continue
			default:
			}

			channel, err := h.fetchChannelDirect(ctx, channelID)
			if err != nil {
				h.logger.Warn("Failed to get channel in batch",
					slog.String("channel_id", channelID),
					slog.Any("error", err),
				)
				resultChan <- struct {
					id      string
					channel *domain.Channel
				}{channelID, nil}
				continue
			}

			resultChan <- struct {
				id      string
				channel *domain.Channel
			}{channelID, channel}
		}
	}

	workerWG.Add(workerCount)
	for range workerCount {
		go worker()
	}

	go func() {
		defer close(jobs)
		for _, id := range missedIDs {
			select {
			case <-ctx.Done():
				return
			case jobs <- id:
			}
		}
	}()

	go func() {
		workerWG.Wait()
		close(resultChan)
	}()

	for {
		select {
		case <-ctx.Done():
			return result, fmt.Errorf("batch channel fetch canceled: %w", ctx.Err())
		case r, ok := <-resultChan:
			if !ok {
				h.logger.Info("GetChannels batch complete (fallback)",
					slog.Int("requested", len(channelIDs)),
					slog.Int("returned", len(result)),
				)
				return result, nil
			}
			if r.channel != nil {
				result[r.id] = r.channel
			}
		}
	}
}
