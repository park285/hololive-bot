package bootstrap

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
)

// bot plane도 runtime이 읽은 YouTube·Holodex 설정을 그대로 쓴다. 코드 기본값 provider 변형을 쓰면
// YOUTUBE_*·HOLODEX_* 운영 설정이 이 plane에서만 조용히 무시된다.
func InitScraperHolodexFoundation(
	ctx context.Context,
	appConfig *settings.Config,
	infra *sharedmodules.InfraModule,
	logger *slog.Logger,
) (*ScraperHolodexFoundation, error) {
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

	return &ScraperHolodexFoundation{
		HolodexService:       holodexService,
		MemberServiceAdapter: memberServiceAdapter,
		SharedRL:             sharedRL,
	}, nil
}
