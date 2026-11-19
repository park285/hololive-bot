package holodexprovider

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"

	streammapping "github.com/kapu/hololive-shared/internal/service/holodex/provider/streammapping"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func (h *Service) GetChannelsLiveStatus(ctx context.Context, channelIDs []string) ([]*domain.Stream, error) {
	if len(channelIDs) == 0 {
		return []*domain.Stream{}, nil
	}

	params := url.Values{}
	params.Set("channels", strings.Join(channelIDs, ","))

	body, err := h.requester.DoRequest(ctx, http.MethodGet, usersLivePath, params)
	if err != nil {
		return nil, fmt.Errorf("get channels live status: channels=%d: %w", len(channelIDs), err)
	}

	streams, err := h.mapChannelsLiveStatus(channelIDs, body)
	if err != nil {
		return nil, fmt.Errorf("map channels live status: %w", err)
	}

	return streams, nil
}

func (h *Service) mapChannelsLiveStatus(channelIDs []string, body []byte) ([]*domain.Stream, error) {
	var rawStreams []streammapping.StreamRaw

	if err := jsonv2.Unmarshal(body, &rawStreams); err != nil {
		return nil, fmt.Errorf("unmarshal channels live status: %w", err)
	}

	streams := h.mapper.MapStreamsResponse(rawStreams)
	h.hydrateIndieStreamChannels(streams, channelIDs)

	filtered := h.filter.FilterHololiveStreams(streams)

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
