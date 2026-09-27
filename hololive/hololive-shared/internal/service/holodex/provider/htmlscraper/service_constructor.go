package htmlscraper

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/park285/shared-go/v2/pkg/httputil"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	scraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping"
)

// NewServiceWithYouTubeClient는 공식 일정 runtime 설정을 env에서 엄격하게 읽어 Service를 만든다.
// 잘못된 값은 기본값으로 바꾸지 않고 오류다.
func NewServiceWithYouTubeClient(
	membersData domain.MemberDataProvider,
	youtubeClient *scraper.Client,
	logger *slog.Logger,
) (*Service, error) {
	runtimeConfig, err := settings.LoadOfficialScheduleRuntimeConfig()
	if err != nil {
		return nil, fmt.Errorf("load official schedule runtime config: %w", err)
	}

	service, err := NewServiceWithOfficialSchedule(membersData, youtubeClient, logger, runtimeConfig)
	if err != nil {
		return nil, fmt.Errorf("new official schedule service: %w", err)
	}

	return service, nil
}

func NewServiceWithOfficialSchedule(
	membersData domain.MemberDataProvider,
	youtubeClient *scraper.Client,
	logger *slog.Logger,
	runtimeConfig settings.OfficialScheduleRuntimeConfig,
) (*Service, error) {
	var source YouTubeClient

	if youtubeClient != nil {
		source = youtubeClient
	}

	service, err := NewServiceWithDependencies(
		membersData,
		ServiceDependencies{YouTube: source},
		logger,
		runtimeConfig,
	)
	if err != nil {
		return nil, fmt.Errorf("new service with dependencies: %w", err)
	}

	return service, nil
}

// NewServiceWithDependencies는 명시한 runtime config와 외부 client로 Service를 구성한다. 공식 일정 식별 색인을 만들 멤버
// 데이터를 적재하지 못하면 빈 색인으로 두지 않고 오류다.
func NewServiceWithDependencies(
	membersData domain.MemberDataProvider,
	dependencies ServiceDependencies,
	logger *slog.Logger,
	runtimeConfig settings.OfficialScheduleRuntimeConfig,
) (*Service, error) {
	if logger == nil {
		logger = slog.Default()
	}

	runtimeConfig = normalizeOfficialScheduleRuntimeConfig(runtimeConfig)

	if dependencies.HTTP == nil {
		dependencies.HTTP = httputil.NewExternalAPIClient(runtimeConfig.OfficialSchedule.Timeout)
	}

	identityIndex, err := buildOfficialScheduleIdentityIndex(membersData)
	if err != nil {
		return nil, fmt.Errorf("new official schedule service: %w", err)
	}

	logger.Info("Official schedule API source initialized",
		slog.String("path", officialScheduleAPIPath),
		slog.Int("identity_keys", len(identityIndex)))

	return &Service{
		httpClient:           dependencies.HTTP,
		identityIndex:        identityIndex,
		logger:               logger,
		officialSchedule:     runtimeConfig.OfficialSchedule,
		maxResponseBodyBytes: runtimeConfig.MaxResponseBodyBytes,
		youtubeClient:        dependencies.YouTube,
	}, nil
}

func normalizeOfficialScheduleRuntimeConfig(config settings.OfficialScheduleRuntimeConfig) settings.OfficialScheduleRuntimeConfig {
	defaults := settings.DefaultOfficialScheduleConfig()

	if strings.TrimSpace(config.OfficialSchedule.BaseURL) == "" {
		config.OfficialSchedule.BaseURL = defaults.BaseURL
	}

	if config.OfficialSchedule.Timeout <= 0 {
		config.OfficialSchedule.Timeout = defaults.Timeout
	}

	if config.OfficialSchedule.PageCacheTTL <= 0 {
		config.OfficialSchedule.PageCacheTTL = defaults.PageCacheTTL
	}

	if config.MaxResponseBodyBytes <= 0 {
		config.MaxResponseBodyBytes = settings.DefaultMaxResponseBodyBytes
	}

	return config
}
