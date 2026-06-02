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
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/kapu/hololive-shared/pkg/util"
)

var ytDefaults = config.DefaultYouTubeOperationalConfig()

type serviceImpl struct {
	service       *youtube.Service
	scraper       *scraper.Client // HTML 스크래퍼 (quota 절약용)
	cache         cache.Client
	logger        *slog.Logger
	quotaUsed     int
	quotaMu       sync.Mutex
	quotaReset    time.Time
	channelToName map[string]string // channelID -> memberName (ChannelTitle 조회용)
	channelMu     sync.RWMutex
}

func New(
	ctx context.Context,
	apiKey string,
	cacheClient cache.Client,
	scraperProxyConfig scraper.ProxyConfig,
	sharedRL *scraper.RateLimiter,
	logger *slog.Logger,
) (Service, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("YouTube API key is required")
	}
	if isPlaceholderYouTubeAPIKey(apiKey) {
		return nil, fmt.Errorf("YouTube API key uses placeholder value")
	}

	service, err := youtube.NewService(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create YouTube service: %w", err)
	}

	ys := &serviceImpl{
		service:       service,
		scraper:       scraper.NewClient(scraper.WithProxy(scraperProxyConfig), scraper.WithRateLimiter(sharedRL)),
		cache:         cacheClient,
		logger:        logger,
		quotaUsed:     0,
		quotaReset:    getNextQuotaReset(),
		channelToName: make(map[string]string),
	}

	// 캐시에서 채널 ID -> 멤버 이름 맵 초기화
	if cacheClient != nil {
		ys.loadChannelNameMap(ctx)
	}

	logger.Info("YouTube backup service initialized",
		slog.Time("quotaReset", ys.quotaReset))

	return ys, nil
}

func isPlaceholderYouTubeAPIKey(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "your_api_key", "your_youtube_api_key", "changeme", "change_me", "replace_me", "replace-with-real-key":
		return true
	default:
		return false
	}
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

// getNextQuotaReset: YouTube API quota 리셋 시간 계산
// YouTube quota는 PST 자정에 리셋됨 = KST 17:00 (UTC+9)
func getNextQuotaReset() time.Time {
	now := time.Now().In(util.KSTZone)

	// KST 17:00 = PST 자정
	resetHour := 17
	next := time.Date(now.Year(), now.Month(), now.Day(), resetHour, 0, 0, 0, util.KSTZone)

	// 이미 오늘 17시가 지났으면 내일 17시
	if now.After(next) {
		next = next.AddDate(0, 0, 1)
	}

	return next
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

func (ys *serviceImpl) checkQuota(cost int) error {
	ys.quotaMu.Lock()
	defer ys.quotaMu.Unlock()

	now := time.Now()
	if now.After(ys.quotaReset) {
		ys.quotaUsed = 0
		ys.quotaReset = getNextQuotaReset()
		ys.logger.Info("YouTube API quota auto-reset",
			slog.Time("nextReset", ys.quotaReset))
	}

	if ys.quotaUsed+cost > (ytDefaults.DailyQuotaLimit - ytDefaults.QuotaSafetyMargin) {
		return &QuotaExceededError{
			Used:      ys.quotaUsed,
			Limit:     ytDefaults.DailyQuotaLimit,
			Requested: cost,
			ResetTime: ys.quotaReset,
		}
	}

	return nil
}

func (ys *serviceImpl) consumeQuota(cost int) {
	ys.quotaMu.Lock()
	defer ys.quotaMu.Unlock()

	ys.quotaUsed += cost
	remaining := ytDefaults.DailyQuotaLimit - ys.quotaUsed

	ys.logger.Debug("YouTube API quota consumed",
		slog.Int("cost", cost),
		slog.Int("used", ys.quotaUsed),
		slog.Int("remaining", remaining),
		slog.Float64("usagePercent", float64(ys.quotaUsed)/float64(ytDefaults.DailyQuotaLimit)*100))

	if remaining < ytDefaults.QuotaSafetyMargin {
		ys.logger.Warn("YouTube API quota running low",
			slog.Int("remaining", remaining),
			slog.Time("resetTime", ys.quotaReset))
	}
}

func (ys *serviceImpl) GetQuotaStatus() (used int, remaining int, resetTime time.Time) {
	ys.quotaMu.Lock()
	defer ys.quotaMu.Unlock()

	if time.Now().After(ys.quotaReset) {
		return 0, ytDefaults.DailyQuotaLimit, getNextQuotaReset()
	}

	return ys.quotaUsed, ytDefaults.DailyQuotaLimit - ys.quotaUsed, ys.quotaReset
}

func (ys *serviceImpl) IsQuotaAvailable(channelCount int) bool {
	estimatedCost := channelCount * ytDefaults.SearchQuotaCost
	err := ys.checkQuota(estimatedCost)
	return err == nil
}
