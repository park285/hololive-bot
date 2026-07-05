package bootstrap

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	sharedproviders "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/park285/iris-client-go/iris"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-shared/pkg/service/activity"
)

type AlarmYouTubeStackComponents struct {
	AlarmMode       *AlarmModeComponents
	Matcher         *matcher.Matcher
	YouTubeStack    *sharedproviders.YouTubeStack
	ActivityLogger  *activity.Logger
	SettingsService settings.ReadWriter
}

func InitAlarmYouTubeStack(
	ctx context.Context,
	appConfig *config.Config,
	infra *sharedmodules.InfraModule,
	foundation *ScraperHolodexProfileFoundation,
	irisClient iris.Sender,
	formatter *adapter.ResponseFormatter,
	logger *slog.Logger,
) (*AlarmYouTubeStackComponents, error) {
	alarmRepository := ProvideAlarmRepository(infra.Postgres, logger)

	alarmMode, err := InitAlarmModeComponents(
		ctx,
		appConfig,
		infra,
		foundation.HolodexService,
		foundation.MemberServiceAdapter,
		alarmRepository,
		logger,
	)
	if err != nil {
		return nil, err
	}

	memberMatcher := ProvideMatcher(
		ctx,
		alarmMode.MemberDataSource,
		infra.Cache,
		foundation.HolodexService,
		logger,
	)
	apiStack := sharedmodules.BuildYouTubeAPIStack(ctx, &sharedmodules.YouTubeAPIStackParams{
		YouTubeConfig:   appConfig.YouTube,
		ScraperConfig:   appConfig.Scraper,
		CacheService:    infra.Cache,
		SharedRateLimit: foundation.SharedRL,
		Logger:          logger,
	})

	return &AlarmYouTubeStackComponents{
		AlarmMode:      alarmMode,
		Matcher:        memberMatcher,
		YouTubeStack:   apiStack,
		ActivityLogger: ProvideActivityLogger(logger),
		SettingsService: sharedmodules.BuildSettingsService(
			appConfig.Notification.AdvanceMinutes,
			appConfig.Scraper.ProxyEnabled,
			logger,
		),
	}, nil
}
