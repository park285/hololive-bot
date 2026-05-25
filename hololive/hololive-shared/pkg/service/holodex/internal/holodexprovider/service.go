package holodexprovider

import (
	"context"
	stdErrors "errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/park285/shared-go/pkg/httputil"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/holodex/internal/holodexprovider/apiclient"
	"github.com/kapu/hololive-shared/pkg/service/holodex/internal/holodexprovider/htmlscraper"
	"github.com/kapu/hololive-shared/pkg/service/holodex/internal/holodexprovider/streammapping"
	"github.com/kapu/hololive-shared/pkg/service/ratelimit"
)

const searchChannelsCacheKeyPrefix = "search_channels:"

var ErrInvalidStreamOrg = stdErrors.New("invalid stream org parameter")

var _ domain.StreamProvider = (*Service)(nil)

type Service struct {
	requester    apiclient.Requester
	scraper      *htmlscraper.Service
	logger       *slog.Logger
	cacheManager *CacheManager
	mapper       *streammapping.StreamMapper
	filter       *streammapping.StreamFilter
	retry        *retryScheduler
	concurrency  config.HolodexConcurrencyConfig
}

func NewHolodexService(baseURL string, apiKey string, cacheClient cache.Client, scraperService *htmlscraper.Service, logger *slog.Logger) (*Service, error) {
	return NewHolodexServiceWithConfig(config.DefaultHolodexOperationalConfig(), baseURL, apiKey, cacheClient, scraperService, logger)
}

func NewHolodexServiceWithConfig(holodexCfg config.HolodexConfig, baseURL string, apiKey string, cacheClient cache.Client, scraperService *htmlscraper.Service, logger *slog.Logger) (*Service, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("holodex api key is required")
	}
	logger.Info("Holodex API key configured")
	httpClient := httputil.NewProfiledClient(httputil.TransportProfile{
		Timeout:             holodexCfg.Timeout,
		MaxConnsPerHost:     holodexCfg.Transport.MaxConnsPerHost,
		MaxIdleConnsPerHost: holodexCfg.Transport.MaxIdleConnsPerHost,
		IdleConnTimeout:     holodexCfg.Transport.IdleConnTimeout,
	})
	var distributedLimiter *ratelimit.SlidingWindowLimiter
	if holodexCfg.DistributedRateLimit.Enabled {
		var err error
		distributedLimiter, err = ratelimit.NewSlidingWindowLimiter(cacheClient, holodexCfg.DistributedRateLimit.KeyPrefix, logger)
		if err != nil {
			return nil, fmt.Errorf("initialize holodex distributed rate limiter: %w", err)
		}
	}
	requester := apiclient.NewHolodexAPIClient(httpClient, baseURL, apiKey, logger, distributedLimiter, holodexCfg)
	service := &Service{
		requester:    requester,
		scraper:      scraperService,
		logger:       logger,
		cacheManager: NewCacheManager(cacheClient, logger),
		mapper:       streammapping.NewStreamMapper(logger),
		filter:       streammapping.NewStreamFilter(logger),
		concurrency:  holodexCfg.Concurrency,
	}
	service.retry = newRetryScheduler(constants.RetrySchedulerConfig.Delay, constants.RetrySchedulerConfig.Timeout, constants.RetrySchedulerConfig.MaxSize, logger)
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

//nolint:contextcheck
func (h *Service) scheduleRetryIfNeeded(ctx context.Context, key string, fn func(ctx context.Context)) {
	if h.retry == nil || isRetryContext(ctx) || ctx.Err() != nil {
		return
	}
	h.retry.schedule(key, fn)
}
