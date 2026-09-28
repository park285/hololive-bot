package holodexprovider

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	streammapping "github.com/kapu/hololive-shared/internal/service/holodex/provider/streammapping"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func (h *Service) fetchHololiveChannelList(ctx context.Context) ([]*domain.Channel, error) {
	if cached, found := h.cacheManager.GetHololiveChannelList(ctx); found {
		return cached, nil
	}

	allChannels, err := h.fetchHololiveChannelListPages(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch hololive channel list pages: %w", err)
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
			return nil, fmt.Errorf("fetch hololive channel list page: %w", err)
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

func (h *Service) fetchHololiveChannelListPage(ctx context.Context, pageSize, offset int) ([]*domain.Channel, int, error) {
	params := url.Values{}
	params.Set("org", constants.HolodexAPIParams.OrgHololive)
	params.Set("type", constants.HolodexAPIParams.TypeVtuber)
	params.Set("limit", fmt.Sprintf("%d", pageSize))
	params.Set("offset", fmt.Sprintf("%d", offset))

	body, err := h.requester.DoRequest(ctx, http.MethodGet, "/channels", params)
	if err != nil {
		return nil, 0, fmt.Errorf("fetch hololive channel list (offset=%d): %w", offset, err)
	}

	var rawChannels []streammapping.ChannelRaw

	if err := jsonv2.Unmarshal(body, &rawChannels); err != nil {
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
