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
	stdErrors "errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/park285/hololive-bot/shared-go/pkg/httputil"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/holodex/internal/holodexprovider/apiclient"
	"github.com/kapu/hololive-shared/pkg/service/ratelimit"
)

const searchChannelsCacheKeyPrefix = "search_channels:"

var ErrInvalidStreamOrg = stdErrors.New("invalid stream org parameter")

var _ domain.StreamProvider = (*Service)(nil)

type ChannelRaw struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	EnglishName     *string `json:"english_name,omitempty"`
	Photo           *string `json:"photo,omitempty"`
	Twitter         *string `json:"twitter,omitempty"`
	VideoCount      *int    `json:"video_count,omitempty"`
	SubscriberCount *int    `json:"subscriber_count,omitempty"`
	Org             *string `json:"org,omitempty"`
	Suborg          *string `json:"suborg,omitempty"`
	Group           *string `json:"group,omitempty"`
}

type StreamRaw struct {
	ID             string              `json:"id"`
	Title          string              `json:"title"`
	ChannelID      *string             `json:"channel_id,omitempty"`
	Status         domain.StreamStatus `json:"status"`
	StartScheduled *string             `json:"start_scheduled,omitempty"`
	StartActual    *string             `json:"start_actual,omitempty"`
	Duration       *int                `json:"duration,omitempty"`
	Link           *string             `json:"link,omitempty"`
	Thumbnail      *string             `json:"thumbnail,omitempty"`
	TopicID        *string             `json:"topic_id,omitempty"`
	LiveViewers    *int                `json:"live_viewers,omitempty"`
	Channel        *ChannelRaw         `json:"channel,omitempty"`
}

// 캐싱 및 스크래핑 폴백(Fallback) 기능을 포함한다.
type Service struct {
	requester    apiclient.Requester
	scraper      *ScraperService
	logger       *slog.Logger
	cacheManager *CacheManager
	mapper       *StreamMapper
	filter       *StreamFilter
	retry        *retryScheduler
}

func NewHolodexService(baseURL string, apiKey string, cacheClient cache.Client, scraperService *ScraperService, logger *slog.Logger) (*Service, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("holodex api key is required")
	}

	logger.Info("Holodex API key configured")

	httpClient := httputil.NewProfiledClient(httputil.TransportProfile{
		Timeout:             constants.APIConfig.HolodexTimeout,
		MaxConnsPerHost:     constants.HolodexTransportConfig.MaxConnsPerHost,
		MaxIdleConnsPerHost: constants.HolodexTransportConfig.MaxIdleConnsPerHost,
		IdleConnTimeout:     constants.HolodexTransportConfig.IdleConnTimeout,
	})

	var distributedLimiter *ratelimit.SlidingWindowLimiter
	if constants.HolodexDistributedRateLimitConfig.Enabled {
		var err error
		distributedLimiter, err = ratelimit.NewSlidingWindowLimiter(
			cacheClient,
			constants.HolodexDistributedRateLimitConfig.KeyPrefix,
			logger,
		)
		if err != nil {
			return nil, fmt.Errorf("initialize holodex distributed rate limiter: %w", err)
		}
	}

	requester := apiclient.NewHolodexAPIClient(httpClient, baseURL, apiKey, logger, distributedLimiter)

	service := &Service{
		requester:    requester,
		scraper:      scraperService,
		logger:       logger,
		cacheManager: NewCacheManager(cacheClient, logger),
		mapper:       NewStreamMapper(logger),
		filter:       NewStreamFilter(logger),
	}
	service.retry = newRetryScheduler(
		constants.RetrySchedulerConfig.Delay,
		constants.RetrySchedulerConfig.Timeout,
		constants.RetrySchedulerConfig.MaxSize,
		logger,
	)
	return service, nil
}

func (h *Service) SetScraperProxyEnabled(enabled bool) bool {
	if h.scraper == nil {
		return false
	}
	return h.scraper.SetYouTubeProxyEnabled(enabled)
}

func (h *Service) ScraperProxyEnabled() bool {
	if h.scraper == nil {
		return false
	}
	return h.scraper.YouTubeProxyEnabled()
}

func (h *Service) Stop() {
	if h.retry != nil {
		h.retry.stop()
	}
}

// scheduleRetryIfNeeded: 재시도가 필요한 경우 지연 재시도를 등록합니다.
// retry는 지연 실행(35s)이므로 원래 context를 전파하지 않고, execute()에서 독립 context를 생성합니다.
//
//nolint:contextcheck // retry callback은 지연 실행되므로 원래 ctx 대신 독립 context 사용
func (h *Service) scheduleRetryIfNeeded(ctx context.Context, key string, fn func(ctx context.Context)) {
	if h.retry == nil || isRetryContext(ctx) || ctx.Err() != nil {
		return
	}
	h.retry.schedule(key, fn)
}
