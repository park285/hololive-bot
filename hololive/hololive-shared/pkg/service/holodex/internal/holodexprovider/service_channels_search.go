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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"

	"github.com/park285/hololive-bot/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func (h *Service) SearchChannels(ctx context.Context, query string) ([]*domain.Channel, error) {
	query = stringutil.TrimSpace(query)
	if cached, found := h.cacheManager.GetSearchChannels(ctx, query); found {
		return cached, nil
	}

	channels, err := h.fetchHololiveChannelList(ctx)
	if err != nil {
		h.logger.Error("Failed to search channels", slog.String("query", query), slog.Any("error", err))
		return nil, fmt.Errorf("search channels: %w", err)
	}

	h.logger.Debug("Holodex API search results",
		slog.String("query", query),
		slog.Int("total_results", len(channels)),
	)

	filtered := filterChannelsByQuery(channels, query, h.filter)

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

func filterChannelsByQuery(channels []*domain.Channel, query string, filter *StreamFilter) []*domain.Channel {
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

func isSearchableHololiveChannel(ch *domain.Channel, filter *StreamFilter) bool {
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
