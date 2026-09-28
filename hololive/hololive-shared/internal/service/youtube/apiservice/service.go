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
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/member"
	youtube "github.com/kapu/hololive-shared/pkg/service/youtube"
	scraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
)

type scraperClient interface {
	GetRecentVideos(ctx context.Context, channelID string, maxResults int) ([]*parser.Video, error)
	GetChannelStats(ctx context.Context, channelID string) (*parser.ChannelStats, error)
}

type serviceImpl struct {
	scraper       scraperClient
	cache         cache.Client
	logger        *slog.Logger
	channelToName map[string]string // channelID -> memberName (ChannelTitle 조회용)
	channelMu     sync.RWMutex

	// runtime YouTube 설정에서 온 timeout이다(YOUTUBE_CACHE_SAVE_TIMEOUT_SECONDS, YOUTUBE_SCRAPER_PHASE_TIMEOUT_SECONDS).
	cacheSaveTimeout    time.Duration
	scraperPhaseTimeout time.Duration
}

func New(
	ctx context.Context,
	cacheClient cache.Client,
	memberData domain.MemberDataProvider,
	youtubeConfig settings.YouTubeConfig,
	sharedRL *ratelimiter.RateLimiter,
	logger *slog.Logger,
) (youtube.Service, error) {
	ys := &serviceImpl{
		scraper:             scraper.NewClient(youtubeConfig, scraper.WithRateLimiter(sharedRL)),
		cache:               cacheClient,
		logger:              logger,
		channelToName:       make(map[string]string),
		cacheSaveTimeout:    youtubeConfig.CacheSaveTimeout,
		scraperPhaseTimeout: youtubeConfig.ScraperPhaseTimeout,
	}

	if memberData != nil {
		ys.loadChannelNameMap(ctx, memberData)
	}

	logger.Info("YouTube scraper service initialized")

	return ys, nil
}

// loadChannelNameMap: 멤버 source 1회 조회 결과로 channelID -> 대표 멤버 이름 맵을 구성한다.
// 이름은 부가 정보이므로 조회 실패는 경고만 남기고 service 생성을 막지 않는다.
func (ys *serviceImpl) loadChannelNameMap(ctx context.Context, memberData domain.MemberDataProvider) {
	members, err := memberData.WithContext(ctx).LoadAllMembers()
	if err != nil {
		ys.logger.Warn("Failed to load members for channel names", slog.Any("error", err))

		return
	}

	ys.channelMu.Lock()
	defer ys.channelMu.Unlock()

	for channelID, representative := range member.ChannelRepresentatives(members) {
		// 대표 이름이 비면 차순위 멤버로 대체하지 않고 resolveChannelTitle의 fallbackTitle을 쓴다.
		if representative.Name == "" {
			continue
		}

		ys.channelToName[channelID] = representative.Name
	}

	ys.logger.Debug("Channel name map loaded", slog.Int("count", len(ys.channelToName)))
}

// getChannelName: channelID로 멤버 이름 조회 (없으면 빈 문자열).
func (ys *serviceImpl) getChannelName(channelID string) string {
	ys.channelMu.RLock()
	defer ys.channelMu.RUnlock()

	return ys.channelToName[channelID]
}
