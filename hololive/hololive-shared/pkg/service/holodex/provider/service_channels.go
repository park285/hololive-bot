package holodexprovider

import (
	"cmp"
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"

	streammapping "github.com/kapu/hololive-shared/internal/service/holodex/provider/streammapping"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func (h *Service) GetChannelSchedule(ctx context.Context, channelID string, hours int, includeLive bool) ([]*domain.Stream, error) {
	if cached, found := h.cacheManager.GetChannelSchedule(ctx, channelID, hours, includeLive); found {
		return h.channelScheduleFromCache(cached, includeLive), nil
	}

	statusStr := channelScheduleStatus(includeLive)

	body, err := h.requester.DoRequest(ctx, http.MethodGet, "/live", channelScheduleParams(channelID, hours, statusStr))
	if err != nil {
		h.logger.Error("Failed to get channel schedule",
			slog.String("channel_id", channelID),
			slog.String("status", statusStr),
			slog.Any("error", err))

		return nil, fmt.Errorf("get channel schedule: %w", err)
	}

	var rawStreams []streammapping.StreamRaw

	if err := jsonv2.Unmarshal(body, &rawStreams); err != nil {
		return nil, fmt.Errorf("unmarshal channel schedule: %w", err)
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
		if stream == nil {
			continue
		}

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

func (h *Service) buildChannelSchedule(rawStreams []streammapping.StreamRaw, includeLive bool) []*domain.Stream {
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
	if stream == nil || stream.StartScheduled == nil {
		return 0
	}

	return stream.StartScheduled.Unix()
}
