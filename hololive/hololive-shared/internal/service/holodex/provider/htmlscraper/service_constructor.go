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

// NewService는 적재한 공식 일정 runtime 설정으로 Service를 구성한다. 전달한 HTTP client가 nil이면 공식 일정 timeout을
// 쓰는 외부 API client를 만든다. 식별 색인용 멤버 데이터를 적재하지 못하면 빈 색인으로 두지 않고 오류를 반환한다.
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
