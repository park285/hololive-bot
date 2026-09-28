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

// GetChannelsLiveStatus는 Holodex /users/live 한 번으로 채널들의 live+upcoming stream을 돌려준다.
// 조직·상태·정렬(org/status/sort) 필터는 적용하지 않는다. 사용 시나리오: 알림 체크, 대시보드 상태 표시 등 빠른 상태 확인.
// 결과는 캐시하지 않는다(요청 채널 집합별 30초 캐시는 적중이 거의 없어 Valkey 책임 축소 A10에서 지웠다).
// 호출마다 Holodex requester의 기존 rate limiter를 거쳐 upstream을 조회한다. 원천 실패는 오류로 그대로 돌려준다. YouTube scraper 2차 경로는
// DEC-20260926-hololive-live-status-scraper-fallback-removal로 삭제했고, alarm-worker는 이 오류를 받으면
// persisted live session으로 판단한다.
func (h *Service) GetChannelsLiveStatus(ctx context.Context, channelIDs []string) ([]*domain.Stream, error) {
	if len(channelIDs) == 0 {
		return []*domain.Stream{}, nil
	}

	params := url.Values{}
	params.Set("channels", strings.Join(channelIDs, ","))

	body, err := h.requester.DoRequest(ctx, http.MethodGet, usersLivePath, params)
	if err != nil {
		h.logger.Error("Failed to get channels live status",
			slog.Int("channel_count", len(channelIDs)),
			slog.Any("error", err),
		)

		return nil, fmt.Errorf("get channels live status: %w", err)
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
		return nil, fmt.Errorf("failed to unmarshal channels live status: %w", err)
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
