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
	"sort"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

type CacheManager struct {
	cache  cache.KeyValueCache
	logger *slog.Logger
}

func NewCacheManager(cacheClient cache.KeyValueCache, logger *slog.Logger) *CacheManager {
	return &CacheManager{
		cache:  cacheClient,
		logger: logger,
	}
}

func (cm *CacheManager) GetLiveStreams(ctx context.Context) ([]*domain.Stream, bool) {
	return cm.GetLiveStreamsByOrg(ctx, constants.HolodexAPIParams.OrgHololive)
}

func (cm *CacheManager) GetLiveStreamsByOrg(ctx context.Context, org string) ([]*domain.Stream, bool) {
	var cached []*domain.Stream
	if err := cm.cache.Get(ctx, buildLiveStreamsCacheKey(org), &cached); err == nil && cached != nil {
		return cached, true
	}
	return nil, false
}

func (cm *CacheManager) SetLiveStreams(ctx context.Context, streams []*domain.Stream) {
	cm.SetLiveStreamsByOrg(ctx, constants.HolodexAPIParams.OrgHololive, streams)
}

func (cm *CacheManager) SetLiveStreamsByOrg(ctx context.Context, org string, streams []*domain.Stream) {
	_ = cm.cache.Set(ctx, buildLiveStreamsCacheKey(org), streams, constants.CacheTTL.LiveStreams)
}

func (cm *CacheManager) GetUpcomingStreams(ctx context.Context, hours int) ([]*domain.Stream, bool) {
	return cm.GetUpcomingStreamsByOrg(ctx, constants.HolodexAPIParams.OrgHololive, hours)
}

func (cm *CacheManager) GetUpcomingStreamsByOrg(ctx context.Context, org string, hours int) ([]*domain.Stream, bool) {
	cacheKey := buildUpcomingStreamsCacheKey(org, hours)
	var cached []*domain.Stream
	if err := cm.cache.Get(ctx, cacheKey, &cached); err == nil && cached != nil {
		return cached, true
	}
	return nil, false
}

func (cm *CacheManager) SetUpcomingStreams(ctx context.Context, hours int, streams []*domain.Stream) {
	cm.SetUpcomingStreamsByOrg(ctx, constants.HolodexAPIParams.OrgHololive, hours, streams)
}

func (cm *CacheManager) SetUpcomingStreamsByOrg(ctx context.Context, org string, hours int, streams []*domain.Stream) {
	cacheKey := buildUpcomingStreamsCacheKey(org, hours)
	_ = cm.cache.Set(ctx, cacheKey, streams, constants.CacheTTL.UpcomingStreams)
}

func (cm *CacheManager) GetChannelSchedule(ctx context.Context, channelID string, hours int, includeLive bool) ([]*domain.Stream, bool) {
	cacheKey := fmt.Sprintf("channel_schedule_%s_%d_%v", channelID, hours, includeLive)
	var cached []*domain.Stream
	if err := cm.cache.Get(ctx, cacheKey, &cached); err == nil && cached != nil {
		return cached, true
	}
	return nil, false
}

func (cm *CacheManager) SetChannelSchedule(ctx context.Context, channelID string, hours int, includeLive bool, streams []*domain.Stream, ttl time.Duration) {
	cacheKey := fmt.Sprintf("channel_schedule_%s_%d_%v", channelID, hours, includeLive)
	_ = cm.cache.Set(ctx, cacheKey, streams, ttl)
}

func (cm *CacheManager) GetSearchChannels(ctx context.Context, query string) ([]*domain.Channel, bool) {
	cacheKey := buildSearchChannelsCacheKey(query)
	var cached []*domain.Channel
	if err := cm.cache.Get(ctx, cacheKey, &cached); err == nil && cached != nil {
		return cached, true
	}
	return nil, false
}

func (cm *CacheManager) SetSearchChannels(ctx context.Context, query string, channels []*domain.Channel) {
	cacheKey := buildSearchChannelsCacheKey(query)
	_ = cm.cache.Set(ctx, cacheKey, channels, constants.CacheTTL.ChannelSearch)
}

func (cm *CacheManager) GetChannel(ctx context.Context, channelID string) (*domain.Channel, bool) {
	cacheKey := fmt.Sprintf("channel_%s", channelID)
	var cached *domain.Channel
	if err := cm.cache.Get(ctx, cacheKey, &cached); err == nil && cached != nil {
		return cached, true
	}
	return nil, false
}

func (cm *CacheManager) SetChannel(ctx context.Context, channelID string, channel *domain.Channel) {
	cacheKey := fmt.Sprintf("channel_%s", channelID)
	_ = cm.cache.Set(ctx, cacheKey, channel, constants.CacheTTL.ChannelInfo)
}

func (cm *CacheManager) GetChannels(ctx context.Context) ([]*domain.Channel, bool) {
	var cached []*domain.Channel
	if err := cm.cache.Get(ctx, "hololive_channels", &cached); err == nil && cached != nil {
		return cached, true
	}
	return nil, false
}

func (cm *CacheManager) SetChannels(ctx context.Context, channels []*domain.Channel) {
	_ = cm.cache.Set(ctx, "hololive_channels", channels, constants.CacheTTL.ChannelInfo)
}

func (cm *CacheManager) GetChannelsLiveStatus(ctx context.Context) (map[string]bool, bool) {
	var cached map[string]bool
	if err := cm.cache.Get(ctx, "channels_live_status", &cached); err == nil && cached != nil {
		return cached, true
	}
	return nil, false
}

func (cm *CacheManager) SetChannelsLiveStatus(ctx context.Context, status map[string]bool) {
	_ = cm.cache.Set(ctx, "channels_live_status", status, constants.CacheTTL.LiveStreams)
}

func (cm *CacheManager) GetChannelsLiveStatusStreams(ctx context.Context, channelIDs []string) ([]*domain.Stream, bool) {
	cacheKey := fmt.Sprintf("channels_live_status_%s", canonicalizeChannelIDsForCache(channelIDs))
	var cached []*domain.Stream
	if err := cm.cache.Get(ctx, cacheKey, &cached); err == nil && cached != nil {
		return cached, true
	}
	return nil, false
}

func (cm *CacheManager) SetChannelsLiveStatusStreams(ctx context.Context, channelIDs []string, streams []*domain.Stream, ttl time.Duration) {
	cacheKey := fmt.Sprintf("channels_live_status_%s", canonicalizeChannelIDsForCache(channelIDs))
	_ = cm.cache.Set(ctx, cacheKey, streams, ttl)
}

func (cm *CacheManager) GetHololiveChannelList(ctx context.Context) ([]*domain.Channel, bool) {
	var cached []*domain.Channel
	if err := cm.cache.Get(ctx, "hololive_channel_list", &cached); err == nil && cached != nil {
		return cached, true
	}
	return nil, false
}

func (cm *CacheManager) SetHololiveChannelList(ctx context.Context, channels []*domain.Channel, ttl time.Duration) {
	_ = cm.cache.Set(ctx, "hololive_channel_list", channels, ttl)
}

func buildLiveStreamsCacheKey(org string) string {
	normalized := normalizeOrgForCache(org)
	if strings.EqualFold(normalized, constants.HolodexAPIParams.OrgHololive) {
		return "live_streams"
	}
	return fmt.Sprintf("live_streams_%s", normalized)
}

func buildUpcomingStreamsCacheKey(org string, hours int) string {
	normalized := normalizeOrgForCache(org)
	if strings.EqualFold(normalized, constants.HolodexAPIParams.OrgHololive) {
		return fmt.Sprintf("upcoming_streams_%d", hours)
	}
	return fmt.Sprintf("upcoming_streams_%s_%d", normalized, hours)
}

func normalizeOrgForCache(org string) string {
	normalized := strings.ToLower(strings.TrimSpace(org))
	if normalized == "" {
		return strings.ToLower(constants.HolodexAPIParams.OrgHololive)
	}
	return normalized
}

func canonicalizeChannelIDsForCache(channelIDs []string) string {
	seen := make(map[string]struct{}, len(channelIDs))
	canonical := make([]string, 0, len(channelIDs))

	for _, channelID := range channelIDs {
		trimmed := strings.TrimSpace(channelID)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		canonical = append(canonical, trimmed)
	}

	sort.Strings(canonical)

	return strings.Join(canonical, ",")
}
