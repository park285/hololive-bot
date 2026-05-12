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
	"fmt"
	"log/slog"
	"net/url"
	"slices"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
)

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
