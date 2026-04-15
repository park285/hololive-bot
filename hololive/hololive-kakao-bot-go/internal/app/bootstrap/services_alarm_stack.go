package bootstrap

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	sharedproviders "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	ytstats "github.com/kapu/hololive-shared/pkg/service/youtube/stats"
	"github.com/park285/iris-client-go/iris"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/activity"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/matcher"
)

type AlarmYouTubeStackComponents struct {
	AlarmMode       *AlarmModeComponents
	MemberMatcher   *matcher.MemberMatcher
	YouTubeStack    *sharedproviders.YouTubeStack
	ActivityLogger  *activity.Logger
	SettingsService settings.ReadWriter
}

func InitAlarmYouTubeStack(
	ctx context.Context,
	cfg *config.Config,
	infra *sharedmodules.InfraModule,
	foundation *ScraperHolodexProfileFoundation,
	irisClient iris.Sender,
	formatter *adapter.ResponseFormatter,
	logger *slog.Logger,
) (*AlarmYouTubeStackComponents, error) {
	alarmRepository := ProvideAlarmRepository(infra.Postgres, logger)

	alarmMode, err := InitAlarmModeComponents(
		ctx,
		cfg,
		infra,
		foundation.HolodexService,
		foundation.MemberServiceAdapter,
		alarmRepository,
		logger,
	)
	if err != nil {
		return nil, err
	}

	memberMatcher := ProvideMemberMatcher(
		ctx,
		alarmMode.MemberDataSource,
		infra.Cache,
		foundation.HolodexService,
		logger,
	)
	youTubeStatsRepository := ytstats.NewYouTubeStatsRepository(infra.Postgres, logger)
	youTubeStack := sharedmodules.BuildYouTubeStack(ctx, sharedmodules.YouTubeStackParams{
		YouTubeConfig:   cfg.YouTube,
		ScraperConfig:   cfg.Scraper,
		CacheService:    infra.Cache,
		HolodexService:  foundation.HolodexService,
		Members:         foundation.MemberServiceAdapter,
		StatsRepo:       youTubeStatsRepository,
		AlarmState:      alarmMode.AlarmService,
		IrisClient:      irisClient,
		Formatter:       formatter,
		SharedRateLimit: foundation.SharedRL,
		Logger:          logger,
	})

	return &AlarmYouTubeStackComponents{
		AlarmMode:      alarmMode,
		MemberMatcher:  memberMatcher,
		YouTubeStack:   youTubeStack,
		ActivityLogger: ProvideActivityLogger(logger),
		SettingsService: sharedmodules.BuildSettingsService(
			cfg.Notification.AdvanceMinutes,
			cfg.Scraper.ProxyEnabled,
			logger,
		),
	}, nil
}
