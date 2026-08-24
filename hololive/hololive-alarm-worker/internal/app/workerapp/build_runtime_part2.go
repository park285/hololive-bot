package workerapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/park285/shared-go/v2/pkg/envutil"

	alarmscheduler "github.com/kapu/hololive-alarm-worker/internal/service/alarm/scheduler"
	"github.com/kapu/hololive-alarm-worker/internal/service/envconfig"
	"github.com/kapu/hololive-alarm-worker/internal/service/workerruntime"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	"github.com/kapu/hololive-shared/pkg/service/notification/alarmservice"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
	scraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping"
)

func buildRuntimeScheduler(
	appConfig *settings.Config,
	cacheClient cache.Client,
	foundation *alarmFoundation,
	logger *slog.Logger,
) (workerruntime.Scheduler, error) {
	if err := validateRuntimeSchedulerInputs(appConfig, foundation); err != nil {
		return nil, fmt.Errorf("validate runtime scheduler inputs: %w", err)
	}

	publishConfig := loadAlarmDispatchPublishConfig(appConfig.AlarmWorkerProfile)

	scheduler, err := alarmscheduler.NewRuntimeScheduler(
		cacheClient,
		foundation.HolodexService,
		foundation.ChzzkClient,
		foundation.TwitchClient,
		foundation.AlarmCRUD,
		foundation.Postgres,
		appConfig.Notification,
		foundation.Outbox,
		publishConfig,
		envutil.Bool("ALARM_TWITCH_ENABLED", true),
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("new alarm worker runtime scheduler: %w", err)
	}

	return scheduler, nil
}

func validateRuntimeSchedulerInputs(appConfig *settings.Config, foundation *alarmFoundation) error {
	if appConfig == nil {
		return errors.New("config is required")
	}

	if foundation == nil {
		return errors.New("alarm foundation is required")
	}

	return nil
}

func runtimeSchedulerDisabled(runtimeRole, configuredRole string, logger *slog.Logger) bool {
	if runtimeAllowsAlarmScheduler(runtimeRole, configuredRole) {
		return false
	}

	if logger != nil {
		logger.Info(
			"Alarm runtime scheduler disabled for this runtime",
			slog.String("runtime_role", runtimeRole),
			slog.String("configured_role", strings.TrimSpace(configuredRole)),
		)
	}

	return true
}

func buildAlarmFoundation(
	ctx context.Context,
	appConfig *settings.Config,
	infra *sharedmodules.InfraModule,
	logger *slog.Logger,
) (*alarmFoundation, error) {
	memberData := providers.ProvideMemberServiceAdapter(ctx, infra.MemberCache, logger)

	sharedRL, err := providers.ProvideYouTubeRateLimiterWithConfig(&appConfig.YouTube, infra.Cache, logger)
	if err != nil {
		return nil, fmt.Errorf("provide youtube producer rate limiter: %w", err)
	}

	scraperService := providers.ProvideScraperServiceWithOfficialSchedule(
		infra.Cache,
		memberData,
		scraper.ProxyConfig{Enabled: appConfig.Scraper.ProxyEnabled, URL: appConfig.Scraper.ProxyURL},
		sharedRL,
		logger,
		appConfig.OfficialScheduleRuntime(),
	)

	holodexService, err := providers.ProvideHolodexServiceWithConfig(&appConfig.Holodex, infra.Cache, scraperService, logger)
	if err != nil {
		return nil, fmt.Errorf("provide holodex service: %w", err)
	}

	chzzkClient := chzzk.NewClientWithConfig(&chzzk.ClientConfig{
		ClientID:     appConfig.Chzzk.ClientID,
		ClientSecret: appConfig.Chzzk.ClientSecret,
		Logger:       logger,
	})
	twitchClient := twitch.NewClient(&twitch.ClientConfig{
		ClientID:     appConfig.Twitch.ClientID,
		ClientSecret: appConfig.Twitch.ClientSecret,
	}, logger)

	alarmRepository := sharedalarm.NewRepository(infra.Postgres, logger)
	outboxRepository := dispatchoutbox.NewPgxRepository(infra.Postgres, logger)
	resolved := sharedmodules.ResolvePersistedTargetMinutes(appConfig.Notification.AdvanceMinutes, appConfig.Scraper.ProxyEnabled, logger)

	alarmService, err := alarmservice.NewAlarmService(infra.Cache, holodexService, chzzkClient, twitchClient, memberData, alarmRepository, logger, resolved)
	if err != nil {
		return nil, fmt.Errorf("create alarm service: %w", err)
	}

	warmAlarmService(ctx, alarmService, logger)

	return &alarmFoundation{
		HolodexService: holodexService,
		ChzzkClient:    chzzkClient,
		TwitchClient:   twitchClient,
		AlarmCRUD:      alarmService,
		AlarmService:   alarmService,
		Outbox:         outboxRepository,
		Postgres:       infra.Postgres,
	}, nil
}

func warmAlarmService(ctx context.Context, alarmService *alarmservice.AlarmService, logger *slog.Logger) {
	if err := alarmService.WarmCacheFromDB(ctx); err != nil {
		logger.Warn("Failed to warm alarm cache from DB", slog.Any("error", err))

		return
	}

	if err := alarmService.SyncPlatformMappings(ctx); err != nil {
		logger.Warn("Failed to sync platform alarm mappings", slog.Any("error", err))
	}
}

func loadAlarmDispatchPublishConfig(profile *settings.AlarmWorkerProfile) queue.PublishConfig {
	return queue.PublishConfig{
		WakeupEnabled:         profile.AlarmDispatch.WakeupEnabled,
		MaxDeliveriesPerBatch: envconfig.ParsePositiveInt("ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH", 1000),
	}
}
