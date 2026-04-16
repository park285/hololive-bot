package bootstrap

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/template"
	ytstats "github.com/kapu/hololive-shared/pkg/service/youtube/stats"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/acl"
)

func InitAdminAPIInfrastructure(ctx context.Context, cfg *config.Config, logger *slog.Logger) (_ *AdminAPIInfrastructure, retErr error) {
	infra, err := InitInfraResources(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}

	defer func() {
		if retErr != nil {
			infra.Cleanup()
		}
	}()

	foundation, err := InitScraperHolodexProfileFoundation(ctx, cfg, infra, logger)
	if err != nil {
		return nil, err
	}

	alarmRepo := ProvideAlarmRepository(infra.Postgres, logger)
	alarmMode, err := InitAlarmModeComponents(
		ctx,
		cfg,
		infra,
		foundation.HolodexService,
		foundation.MemberServiceAdapter,
		alarmRepo,
		logger,
	)
	if err != nil {
		return nil, err
	}

	aclService, err := ProvideACLService(
		ctx,
		cfg.Kakao.ACLEnabled,
		acl.ParseACLMode(cfg.Kakao.ACLMode),
		cfg.Kakao.Rooms,
		infra.Postgres,
		infra.Cache,
		logger,
	)
	if err != nil {
		return nil, err
	}

	statsRepo := ytstats.NewYouTubeStatsRepository(infra.Postgres, logger)
	ytStack := sharedmodules.BuildYouTubeAPIStack(ctx, sharedmodules.YouTubeAPIStackParams{
		YouTubeConfig:   cfg.YouTube,
		ScraperConfig:   cfg.Scraper,
		CacheService:    infra.Cache,
		StatsRepo:       statsRepo,
		SharedRateLimit: foundation.SharedRL,
		Logger:          logger,
	})

	templateRenderer := template.NewRenderer(infra.Postgres.GetGormDB(), logger)

	return &AdminAPIInfrastructure{
		Cache:            infra.Cache,
		Postgres:         infra.Postgres,
		MemberRepo:       infra.MemberRepo,
		MemberCache:      infra.MemberCache,
		Profiles:         foundation.ProfileService,
		AlarmCRUD:        alarmMode.AlarmCRUD,
		HolodexService:   foundation.HolodexService,
		YouTubeService:   ytStack.GetService(),
		StatsRepo:        ytStack.GetStatsRepo(),
		ActivityLogger:   ProvideActivityLogger(logger),
		SettingsService:  sharedmodules.BuildSettingsService(cfg.Notification.AdvanceMinutes, cfg.Scraper.ProxyEnabled, logger),
		ACLService:       aclService,
		TemplateAdminSvc: BuildTemplateAdminService(infra, templateRenderer, logger),
		Cleanup:          infra.Cleanup,
	}, nil
}
