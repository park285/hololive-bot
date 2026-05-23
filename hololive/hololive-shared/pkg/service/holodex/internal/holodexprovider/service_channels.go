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
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"slices"

	"github.com/park285/hololive-bot/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// includeLive가 true이면 현재 진행 중인 방송도 포함한다.
func (h *Service) GetChannelSchedule(ctx context.Context, channelID string, hours int, includeLive bool) ([]*domain.Stream, error) {
	if cached, found := h.cacheManager.GetChannelSchedule(ctx, channelID, hours, includeLive); found {
		return h.channelScheduleFromCache(cached, includeLive), nil
	}

	statusStr := channelScheduleStatus(includeLive)
	body, err := h.requester.DoRequest(ctx, "GET", "/live", channelScheduleParams(channelID, hours, statusStr))
	if err != nil {
		return h.handleChannelScheduleRequestError(ctx, channelID, statusStr, err)
	}

	var rawStreams []StreamRaw
	if err := json.Unmarshal(body, &rawStreams); err != nil {
		return nil, fmt.Errorf("failed to unmarshal channel schedule: %w", err)
	}

	result := h.buildChannelSchedule(rawStreams, includeLive)

	h.cacheManager.SetChannelSchedule(ctx, channelID, hours, includeLive, result, constants.CacheTTL.ChannelSchedule)

	return result, nil
}

func (h *Service) channelScheduleFromCache(cached []*domain.Stream, includeLive bool) []*domain.Stream {
	copied := copyChannelScheduleStreams(cached)
	if includeLive {
		return copied
	}
	return h.filter.FilterUpcomingStreams(copied)
}

func copyChannelScheduleStreams(streams []*domain.Stream) []*domain.Stream {
	copied := make([]*domain.Stream, len(streams))
	for i, stream := range streams {
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
	return copied
}

func channelScheduleStatus(includeLive bool) string {
	if includeLive {
		return string(domain.StreamStatusLive) + "," + string(domain.StreamStatusUpcoming)
	}
	return string(domain.StreamStatusUpcoming)
}

func channelScheduleParams(channelID string, hours int, statusStr string) url.Values {
	params := url.Values{}
	params.Set("channel_id", channelID)
	params.Set("status", statusStr)
	params.Set("type", "stream")
	params.Set("max_upcoming_hours", fmt.Sprintf("%d", hours))
	return params
}

func (h *Service) handleChannelScheduleRequestError(ctx context.Context, channelID string, statusStr string, err error) ([]*domain.Stream, error) {
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

func (h *Service) buildChannelSchedule(rawStreams []StreamRaw, includeLive bool) []*domain.Stream {
	allStreams := h.mapper.MapStreamsResponse(rawStreams)
	hololiveOnly := h.filter.FilterHololiveStreams(allStreams)
	sortStreamsByScheduledTime(hololiveOnly)
	if includeLive {
		return hololiveOnly
	}
	return h.filter.FilterUpcomingStreams(hololiveOnly)
}

func sortStreamsByScheduledTime(streams []*domain.Stream) {
	slices.SortFunc(streams, func(a, b *domain.Stream) int {
		return cmp.Compare(streamScheduledUnix(a), streamScheduledUnix(b))
	})
}

func streamScheduledUnix(stream *domain.Stream) int64 {
	if stream.StartScheduled == nil {
		return 0
	}
	return stream.StartScheduled.Unix()
}
