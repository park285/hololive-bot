package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
)

// admin plane도 runtime이 읽은 YouTube·Holodex 설정을 그대로 쓴다. 코드 기본값 provider 변형을 쓰면
// YOUTUBE_*·HOLODEX_* 운영 설정이 이 plane에서만 조용히 무시된다.
func buildScraperHolodexFoundation(
	ctx context.Context,
	appConfig *settings.Config,
	infra *sharedmodules.InfraModule,
	logger *slog.Logger,
) (*scraperHolodexFoundation, error) {
	memberServiceAdapter := providers.ProvideMemberServiceAdapter(ctx, infra.MemberCache, logger)

	sharedRL, err := providers.ProvideYouTubeRateLimiterWithConfig(&appConfig.YouTube, infra.Cache, logger)
	if err != nil {
		return nil, fmt.Errorf("provide youtube producer rate limiter: %w", err)
	}

	scraperService, err := providers.ProvideScraperServiceWithOfficialSchedule(
		memberServiceAdapter,
		appConfig.YouTube,
		sharedRL,
		logger,
		appConfig.OfficialScheduleRuntime(),
	)
	if err != nil {
		return nil, fmt.Errorf("provide scraper service: %w", err)
	}

	holodexService, err := providers.ProvideHolodexServiceWithConfig(&appConfig.Holodex, infra.Cache, scraperService, logger)
	if err != nil {
		return nil, fmt.Errorf("provide holodex service: %w", err)
	}

	return &scraperHolodexFoundation{
		HolodexService:       holodexService,
		MemberServiceAdapter: memberServiceAdapter,
		SharedRL:             sharedRL,
	}, nil
}

// Alarm 데이터 원천은 alarm-worker HTTP provider 하나다(hololive-api bot·admin plane 공통).
// ALARM_INTERNAL_URL은 apiplane.LoadRuntime이 필수로 검증하므로 in-process AlarmService 분기는 두지 않고,
// URL이 비면 오류로 끝낸다.
func buildAlarmModeComponents(
	appConfig *settings.Config,
	memberData domain.MemberDataProvider,
	logger *slog.Logger,
) (*alarmModeComponents, error) {
	providerURL := strings.TrimSpace(appConfig.AlarmServiceURL)
	if providerURL == "" {
		return nil, errors.New("alarm provider URL (ALARM_INTERNAL_URL) is required")
	}

	alarmClient, err := sharedalarm.NewClientWithAPIKeyStrict(providerURL, appConfig.Server.APIKey, logger)
	if err != nil {
		return nil, fmt.Errorf("configure alarm worker client: %w", err)
	}

	return &alarmModeComponents{
		AlarmCRUD:        alarmClient,
		MemberDataSource: memberData,
	}, nil
}
