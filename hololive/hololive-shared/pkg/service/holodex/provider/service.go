package holodexprovider

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/park285/shared-go/v2/pkg/httputil"

	"github.com/kapu/hololive-shared/internal/service/holodex/provider/apiclient"
	"github.com/kapu/hololive-shared/internal/service/holodex/provider/htmlscraper"
	"github.com/kapu/hololive-shared/internal/service/holodex/provider/streammapping"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/httpclientmetrics"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	ratelimitvalkey "github.com/kapu/hololive-shared/pkg/service/ratelimit/valkey"
)

const usersLivePath = "/users/live"

var ErrInvalidStreamOrg = errors.New("invalid stream org parameter")

var _ domain.StreamProvider = (*Service)(nil)

type Service struct {
	requester    apiclient.Requester
	scraper      *htmlscraper.Service
	logger       *slog.Logger
	cacheManager *CacheManager
	mapper       *streammapping.StreamMapper
	filter       *streammapping.StreamFilter
	retry        *retryScheduler
	concurrency  settings.HolodexConcurrencyConfig

	streamCacheFills streamCacheFillGate
}

func NewHolodexService(baseURL, apiKey string, cacheClient cache.Client, scraperService *htmlscraper.Service, logger *slog.Logger) (*Service, error) {
	cfg := settings.DefaultHolodexOperationalConfig()

	service, err := NewHolodexServiceWithConfig(&cfg, baseURL, apiKey, cacheClient, scraperService, logger)
	if err != nil {
		return nil, fmt.Errorf("holodex service with config: %w", err)
	}

	return service, nil
}

func NewHolodexServiceWithConfig(holodexCfg *settings.HolodexConfig, baseURL, apiKey string, cacheClient cache.Client, scraperService *htmlscraper.Service, logger *slog.Logger) (*Service, error) {
	if holodexCfg == nil {
		return nil, errors.New("holodex config is nil")
	}

	if err := settings.ValidateHolodexRequestConfig(holodexCfg); err != nil {
		return nil, fmt.Errorf("validate holodex request config: %w", err)
	}

	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("holodex api key is required")
	}

	logger.Info("Holodex API key configured")

	httpClient := httputil.NewProfiledClient(httputil.TransportProfile{
		Timeout:             holodexCfg.Timeout,
		MaxConnsPerHost:     holodexCfg.Transport.MaxConnsPerHost,
		MaxIdleConnsPerHost: holodexCfg.Transport.MaxIdleConnsPerHost,
		IdleConnTimeout:     holodexCfg.Transport.IdleConnTimeout,
	})

	var distributedLimiter *ratelimitvalkey.SlidingWindowLimiter

	if holodexCfg.DistributedRateLimit.Enabled {
		var err error

		distributedLimiter, err = ratelimitvalkey.NewSlidingWindowLimiter(cacheClient, holodexCfg.DistributedRateLimit.KeyPrefix, logger)
		if err != nil {
			return nil, fmt.Errorf("initialize holodex distributed rate limiter: %w", err)
		}
	}

	requester, err := apiclient.NewHolodexAPIClient(httpClient, baseURL, apiKey, logger, distributedLimiter, holodexCfg)
	if err != nil {
		return nil, fmt.Errorf("initialize holodex API client: %w", err)
	}

	// API client가 *http.Transport에 HTTP/2 설정을 적용한 뒤 감싼다. 같은 *http.Client를 공유하므로
	// 이후 모든 Holodex 요청이 hololive_http_client_*{client="holodex"}에 기록된다.
	httpClient.Transport = httpclientmetrics.Instrument("holodex", httpClient.Transport)

	service := &Service{
		requester:    requester,
		scraper:      scraperService,
		logger:       logger,
		cacheManager: NewCacheManager(cacheClient, logger),
		mapper:       streammapping.NewStreamMapper(logger),
		filter:       streammapping.NewStreamFilter(logger),
		concurrency:  holodexCfg.Concurrency,
	}

	service.retry = newRetryScheduler(retrySchedulerDelay, retrySchedulerTimeout, retrySchedulerMaxSize, logger)

	return service, nil
}

func (h *Service) Stop() {
	if h.retry != nil {
		h.retry.stop()
	}
}

func (h *Service) scheduleRetryIfNeeded(ctx context.Context, key string, fn func(ctx context.Context)) {
	if h.retry == nil || isRetryContext(ctx) || ctx.Err() != nil {
		return
	}

	h.retry.schedule(ctx, key, fn)
}
