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
	"strings"

	sharedlog "github.com/park285/shared-go/v2/pkg/logging"
	"github.com/park285/shared-go/v2/pkg/stringutil"

	streammapping "github.com/kapu/hololive-shared/internal/service/holodex/provider/streammapping"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/privacylog"
)

func (h *Service) SearchChannels(ctx context.Context, query string) ([]*domain.Channel, error) {
	query = stringutil.TrimSpace(query)

	// 검색 결과를 query별로 다시 캐시하지 않는다. 목록은 hololive_channel_list 캐시를 거치므로
	// 매 요청마다 그 목록을 필터링해도 upstream 호출은 늘지 않는다(Valkey 책임 축소 A10).
	channels, err := h.fetchHololiveChannelList(ctx)
	if err != nil {
		if logErr := sharedlog.LogAndWrapError(ctx, h.logger, "search channels", err, searchQueryAttr(query)); logErr != nil {
			return nil, fmt.Errorf("log and wrap error: %w", logErr)
		}

		return nil, nil
	}

	h.logger.Debug("Holodex API search results",
		searchQueryAttr(query),
		slog.Int("total_results", len(channels)),
	)

	filtered := filterChannelsByQuery(channels, query, h.filter)

	h.logger.Debug("After HOLOSTARS filter", slog.Int("count", len(filtered)))

	return filtered, nil
}

func searchQueryAttr(query string) slog.Attr {
	return slog.String("query_token", privacylog.Pseudonym(query))
}

func filterChannelsByQuery(channels []*domain.Channel, query string, filter *streammapping.StreamFilter) []*domain.Channel {
	filtered := make([]*domain.Channel, 0, len(channels))
	normalizedQuery := strings.ToLower(stringutil.TrimSpace(query))

	for _, ch := range channels {
		if !isSearchableHololiveChannel(ch, filter) {
			continue
		}

		if normalizedQuery == "" {
			filtered = append(filtered, ch)
			continue
		}

		if channelMatchesSearchQuery(ch, normalizedQuery) {
			filtered = append(filtered, ch)
		}
	}

	return filtered
}

func isSearchableHololiveChannel(ch *domain.Channel, filter *streammapping.StreamFilter) bool {
	if ch == nil {
		return false
	}

	return ch.Org != nil && *ch.Org == constants.HolodexAPIParams.OrgHololive && !filter.IsHolostarsChannel(ch)
}

func channelMatchesSearchQuery(ch *domain.Channel, normalizedQuery string) bool {
	if strings.Contains(strings.ToLower(ch.Name), normalizedQuery) {
		return true
	}

	if ch.EnglishName != nil && strings.Contains(strings.ToLower(*ch.EnglishName), normalizedQuery) {
		return true
	}

	return strings.Contains(strings.ToLower(ch.ID), normalizedQuery)
}

// retryable Holodex 오류(5xx/timeout/circuit/key rotation)에서만 YouTube 스크래퍼로 폴백하고,
// non-retryable 오류는 그대로 반환합니다.
