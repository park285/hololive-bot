package app

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/iris"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/settings"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/activity"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/matcher"
)

type alarmYouTubeStackComponents struct {
	alarmMode       *alarmModeComponents
	memberMatcher   *matcher.MemberMatcher
	youTubeStack    *providers.YouTubeStack
	activityLogger  *activity.Logger
	settingsService settings.ReadWriter
}

func initAlarmYouTubeStack(
	ctx context.Context,
	cfg *config.Config,
	infra *infraResources,
	foundation *scraperHolodexProfileFoundation,
	irisClient iris.Client,
	formatter *adapter.ResponseFormatter,
	logger *slog.Logger,
) (*alarmYouTubeStackComponents, error) {
	alarmRepository := ProvideAlarmRepository(infra.postgresService, logger)
	alarmMode, err := initAlarmModeComponents(
		ctx,
		cfg,
		infra,
		foundation.holodexService,
		foundation.memberServiceAdapter,
		alarmRepository,
		logger,
	)
	if err != nil {
		return nil, err
	}

	memberMatcher := ProvideMemberMatcher(
		ctx,
		alarmMode.memberDataSource,
		infra.cacheService,
		foundation.holodexService,
		logger,
	)
	youTubeStatsRepository := providers.ProvideYouTubeStatsRepository(infra.postgresService, logger)
	youTubeStack := providers.ProvideYouTubeStack(
		ctx,
		cfg.YouTube,
		cfg.Scraper,
		infra.cacheService,
		foundation.holodexService,
		foundation.memberServiceAdapter,
		youTubeStatsRepository,
		alarmMode.alarmService,
		irisClient,
		formatter,
		foundation.sharedRL,
		logger,
	)

	return &alarmYouTubeStackComponents{
		alarmMode:      alarmMode,
		memberMatcher:  memberMatcher,
		youTubeStack:   youTubeStack,
		activityLogger: ProvideActivityLogger(logger),
		settingsService: providers.ProvideSettingsService(
			cfg.Notification.AdvanceMinutes,
			cfg.Scraper.ProxyEnabled,
			logger,
		),
	}, nil
}
