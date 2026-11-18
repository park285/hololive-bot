package htmlscraper

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/park285/shared-go/v2/pkg/httputil"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func NewService(
	ctx context.Context,
	membersData domain.MemberDataProvider,
	httpClient *http.Client,
	logger *slog.Logger,
	runtimeConfig settings.OfficialScheduleRuntimeConfig,
) (*Service, error) {
	if logger == nil {
		logger = slog.Default()
	}

	runtimeConfig = normalizeOfficialScheduleRuntimeConfig(runtimeConfig)

	if httpClient == nil {
		httpClient = httputil.NewExternalAPIClient(runtimeConfig.OfficialSchedule.Timeout)
	}

	identityIndex, err := buildOfficialScheduleIdentityIndex(ctx, membersData)
	if err != nil {
		return nil, fmt.Errorf("new official schedule service: %w", err)
	}

	logger.Info("Official schedule API source initialized",
		slog.String("path", officialScheduleAPIPath),
		slog.Int("identity_keys", len(identityIndex)))

	return &Service{
		httpClient:           httpClient,
		identityIndex:        identityIndex,
		logger:               logger,
		officialSchedule:     runtimeConfig.OfficialSchedule,
		maxResponseBodyBytes: runtimeConfig.MaxResponseBodyBytes,
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
