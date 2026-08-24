package htmlscraper

import (
	"log/slog"
	"strings"

	"github.com/park285/shared-go/v2/pkg/httputil"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	scraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
)

func NewService(
	cacheClient cache.StreamCache,
	membersData domain.MemberDataProvider,
	youtubeProxyConfig scraper.ProxyConfig,
	sharedRL *ratelimiter.RateLimiter,
	logger *slog.Logger,
) *Service {
	return NewServiceWithYouTubeClient(
		cacheClient,
		membersData,
		scraper.NewClient(scraper.WithProxy(youtubeProxyConfig), scraper.WithRateLimiter(sharedRL)),
		logger,
	)
}

func NewServiceWithYouTubeClient(
	cacheClient cache.StreamCache,
	membersData domain.MemberDataProvider,
	youtubeClient *scraper.Client,
	logger *slog.Logger,
) *Service {
	return NewServiceWithOfficialSchedule(
		cacheClient,
		membersData,
		youtubeClient,
		logger,
		settings.LoadOfficialScheduleRuntimeConfig(),
	)
}

func NewServiceWithOfficialSchedule(
	cacheClient cache.StreamCache,
	membersData domain.MemberDataProvider,
	youtubeClient *scraper.Client,
	logger *slog.Logger,
	runtimeConfig settings.OfficialScheduleRuntimeConfig,
) *Service {
	var source YouTubeClient

	if youtubeClient != nil {
		source = youtubeClient
	}

	return NewServiceWithDependencies(
		cacheClient,
		membersData,
		ServiceDependencies{YouTube: source},
		logger,
		runtimeConfig,
	)
}

// NewServiceWithDependencies는 명시한 runtime config와 외부 client로 Service를 구성한다.
func NewServiceWithDependencies(
	cacheClient cache.StreamCache,
	membersData domain.MemberDataProvider,
	dependencies ServiceDependencies,
	logger *slog.Logger,
	runtimeConfig settings.OfficialScheduleRuntimeConfig,
) *Service {
	if logger == nil {
		logger = slog.Default()
	}

	runtimeConfig = normalizeOfficialScheduleRuntimeConfig(runtimeConfig)

	if dependencies.HTTP == nil {
		dependencies.HTTP = httputil.NewExternalAPIClient(runtimeConfig.OfficialSchedule.Timeout)
	}

	identityIndex := buildOfficialScheduleIdentityIndex(membersData)
	logger.Info("Official schedule API source initialized",
		slog.String("path", officialScheduleAPIPath),
		slog.Int("identity_keys", len(identityIndex)))

	return &Service{
		httpClient:           dependencies.HTTP,
		cache:                cacheClient,
		identityIndex:        identityIndex,
		logger:               logger,
		officialSchedule:     runtimeConfig.OfficialSchedule,
		maxResponseBodyBytes: runtimeConfig.MaxResponseBodyBytes,
		youtubeClient:        dependencies.YouTube,
	}
}

func normalizeOfficialScheduleRuntimeConfig(config settings.OfficialScheduleRuntimeConfig) settings.OfficialScheduleRuntimeConfig {
	defaults := settings.DefaultOfficialScheduleConfig()

	if strings.TrimSpace(config.OfficialSchedule.BaseURL) == "" {
		config.OfficialSchedule.BaseURL = defaults.BaseURL
	}

	if config.OfficialSchedule.Timeout <= 0 {
		config.OfficialSchedule.Timeout = defaults.Timeout
	}

	if config.OfficialSchedule.CacheExpiry <= 0 {
		config.OfficialSchedule.CacheExpiry = defaults.CacheExpiry
	}

	if config.OfficialSchedule.PageCacheTTL <= 0 {
		config.OfficialSchedule.PageCacheTTL = defaults.PageCacheTTL
	}

	if config.MaxResponseBodyBytes <= 0 {
		config.MaxResponseBodyBytes = settings.DefaultMaxResponseBodyBytes
	}

	return config
}
