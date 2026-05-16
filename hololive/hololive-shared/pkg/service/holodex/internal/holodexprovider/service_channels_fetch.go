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

	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/domain"
)

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

// 캐시를 우선 조회하고, 캐시 미스된 채널은 /channels 리스트 API로 한 번에 조회합니다.
// 기존 N+1 개별 호출 패턴을 단일 호출로 최적화하여 rate limit 부담을 대폭 감소시킵니다.
