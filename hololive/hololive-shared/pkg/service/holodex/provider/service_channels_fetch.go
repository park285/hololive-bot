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

	sharedlog "github.com/park285/shared-go/v2/pkg/logging"

	streammapping "github.com/kapu/hololive-shared/internal/service/holodex/provider/streammapping"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// GetChannel은 Holodex /channels/{id} 하나만 원천으로 쓴다. 실패는 그대로 돌려주고, YouTube scraper로 만든 부분
// Channel로 보충하거나 캐시하지 않는다(DEC-20260926-hololive-source-fallbacks-retirement).
func (h *Service) GetChannel(ctx context.Context, channelID string) (*domain.Channel, error) {
	if cached, found := h.cacheManager.GetChannel(ctx, channelID); found {
		return cached, nil
	}

	channel, err := h.fetchChannelDirect(ctx, channelID)
	if err == nil {
		return channel, nil
	}

	return nil, sharedlog.LogAndWrapError(ctx, h.logger, "get channel", err, slog.String("channel_id", channelID))
}

func (h *Service) fetchChannelDirect(ctx context.Context, channelID string) (*domain.Channel, error) {
	body, err := h.requester.DoRequest(ctx, http.MethodGet, "/channels/"+channelID, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch channel direct: %w", err)
	}

	var rawChannel streammapping.ChannelRaw

	if err := jsonv2.Unmarshal(body, &rawChannel); err != nil {
		return nil, fmt.Errorf("failed to unmarshal channel: %w", err)
	}

	channel := h.mapper.MapChannelResponse(&rawChannel)
	h.cacheManager.SetChannel(ctx, channelID, channel)

	return channel, nil
}
