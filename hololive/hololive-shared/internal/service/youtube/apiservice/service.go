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

package apiservice

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

var ytDefaults = config.DefaultYouTubeOperationalConfig()

type scraperClient interface {
	GetRecentVideos(ctx context.Context, channelID string, maxResults int) ([]*scraper.Video, error)
	GetChannelStats(ctx context.Context, channelID string) (*scraper.ChannelStats, error)
	SetProxyEnabled(enabled bool) bool
	ProxyEnabled() bool
}

type serviceImpl struct {
	scraper       scraperClient
	cache         cache.Client
	logger        *slog.Logger
	channelToName map[string]string // channelID -> memberName (ChannelTitle 조회용)
	channelMu     sync.RWMutex
}

func New(
	ctx context.Context,
	cacheClient cache.Client,
	scraperProxyConfig scraper.ProxyConfig,
	sharedRL *scraper.RateLimiter,
	logger *slog.Logger,
) (Service, error) {
	ys := &serviceImpl{
		scraper:       scraper.NewClient(scraper.WithProxy(scraperProxyConfig), scraper.WithRateLimiter(sharedRL)),
		cache:         cacheClient,
		logger:        logger,
		channelToName: make(map[string]string),
	}

	// 캐시에서 채널 ID -> 멤버 이름 맵 초기화
	if cacheClient != nil {
		ys.loadChannelNameMap(ctx)
	}

	logger.Info("YouTube scraper service initialized")

	return ys, nil
}

func (ys *serviceImpl) SetScraperProxyEnabled(enabled bool) bool {
	if ys == nil || ys.scraper == nil {
		return false
	}
	return ys.scraper.SetProxyEnabled(enabled)
}

func (ys *serviceImpl) ScraperProxyEnabled() bool {
	if ys == nil || ys.scraper == nil {
		return false
	}
	return ys.scraper.ProxyEnabled()
}

// loadChannelNameMap: 캐시에서 멤버 정보를 읽어 channelID -> memberName 맵을 구성
func (ys *serviceImpl) loadChannelNameMap(ctx context.Context) {
	if ys.cache == nil {
		return
	}

	memberMap, err := ys.cache.GetAllMembers(ctx)
	if err != nil {
		ys.logger.Warn("Failed to load member map for channel names", slog.Any("error", err))
		return
	}

	ys.channelMu.Lock()
	defer ys.channelMu.Unlock()

	ys.storeChannelNameMap(memberMap)

	ys.logger.Debug("Channel name map loaded", slog.Int("count", len(ys.channelToName)))
}

func (ys *serviceImpl) storeChannelNameMap(memberMap map[string]string) {
	for key, channelID := range memberMap {
		if channelID == "" {
			continue
		}
		ys.channelToName[channelID] = memberNameFromCacheKey(key)
	}
}

func memberNameFromCacheKey(key string) string {
	if idx := strings.LastIndex(key, ":"); idx > 0 {
		return key[:idx]
	}
	return key
}

// getChannelName: channelID로 멤버 이름 조회 (없으면 빈 문자열)
func (ys *serviceImpl) getChannelName(channelID string) string {
	ys.channelMu.RLock()
	defer ys.channelMu.RUnlock()
	return ys.channelToName[channelID]
}
