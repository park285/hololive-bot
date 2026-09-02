package workerapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/park285/iris-client-go/v2/iris"
	envutil "github.com/park285/shared-go/v2/pkg/envutil"

	"github.com/kapu/hololive-alarm-worker/internal/egress"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch"
	"github.com/kapu/hololive-alarm-worker/internal/service/dispatchrun"
	"github.com/kapu/hololive-alarm-worker/internal/service/workerruntime"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/service/alarm/handoff"
	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/service/kakaoroom"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/dispatchstate"
)

func buildNotificationEgress(
	ctx context.Context,
	appConfig *settings.Config,
	infra *sharedmodules.InfraModule,
	logger *slog.Logger,
	workerState *alarmWorkerRegistryState,
) (workerruntime.Scheduler, error) {
	if appConfig == nil {
		return nil, errors.New("config is required")
	}

	if infra == nil || infra.Postgres == nil {
		return nil, errors.New("postgres is required")
	}

	irisClient, err := providers.ProvideIrisKaringClient(
		logger,
		iris.WithBaseURL(appConfig.Iris.BaseURL),
		iris.WithBotToken(appConfig.Iris.BotToken),
	)
	if err != nil {
		return nil, fmt.Errorf("init alarm-worker notification egress iris client: %w", err)
	}

	rooms := kakaoroom.New(infra.Postgres.GetPool(), kakaoroom.ListerFrom(irisClient), logger)
	irisSender := egress.NewIrisMessageSender(
		irisClient,
		egress.WithMarkdownReplies(appConfig.Bot.MarkdownReplies),
		egress.WithRoomChat(rooms),
	)

	runners, err := buildEgressRunners(ctx, appConfig, infra, irisSender, logger, workerState)
	if err != nil {
		return nil, fmt.Errorf("build egress runners: %w", err)
	}

	return workerruntime.NewNotificationEgressRunner(runners, logger), nil
}

func buildEgressRunners(
	ctx context.Context,
	appConfig *settings.Config,
	infra *sharedmodules.InfraModule,
	irisSender *egress.IrisMessageSender,
	logger *slog.Logger,
	workerState *alarmWorkerRegistryState,
) ([]workerruntime.NamedScheduler, error) {
	runners, err := appendAlarmDispatchRunner(ctx, nil, appConfig, infra, irisSender, logger, workerState)
	if err != nil {
		return nil, fmt.Errorf("append alarm dispatch runner: %w", err)
	}

	runners = append(runners, workerruntime.NamedScheduler{
		Name:      "alarm-dispatch-maintenance",
		Scheduler: dispatchrun.NewMaintenanceRunner(infra, appConfig.AlarmDispatchRetention, logger),
	})

	mode, youtubeEnabled, err := youtubeOutboxHandoffActivation(appConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("resolve youtube outbox handoff activation: %w", err)
	}

	if youtubeEnabled {
		sender := buildYouTubeOutboxSender(irisSender, alarmDispatchMessageStrings(infra, logger))

		dispatcher, buildErr := buildYouTubeOutboxDispatcher(appConfig, infra, sender, logger, workerState, mode)
		if buildErr != nil {
			return nil, fmt.Errorf("build youtube outbox dispatcher: %w", buildErr)
		}

		runners = append(runners, workerruntime.NamedScheduler{
			Name:      "youtube-outbox",
			Scheduler: workerState.wrap("youtube_delivery", dispatcher),
		})
	}

	runners, err = appendNotificationDeliveryRunner(runners, appConfig, infra, irisSender, logger, workerState)
	if err != nil {
		return nil, fmt.Errorf("append notification delivery runner: %w", err)
	}

	return runners, nil
}

func appendAlarmDispatchRunner(
	ctx context.Context,
	runners []workerruntime.NamedScheduler,
	appConfig *settings.Config,
	infra *sharedmodules.InfraModule,
	irisSender *egress.IrisMessageSender,
	logger *slog.Logger,
	workerState *alarmWorkerRegistryState,
) ([]workerruntime.NamedScheduler, error) {
	if !alarmWorkerExecutorEnabled(appConfig, "alarm_dispatch") {
		logWorkerDisabled(logger, "Alarm dispatch consumer disabled")

		return runners, nil
	}

	runner, err := buildAlarmDispatchRunner(ctx, appConfig, infra, irisSender, logger, workerState)
	if err != nil {
		return nil, fmt.Errorf("build alarm dispatch runner: %w", err)
	}

	return append(runners, workerruntime.NamedScheduler{
		Name:      "alarm-dispatch",
		Scheduler: workerState.wrap("alarm_dispatch", runner),
	}), nil
}

func appendNotificationDeliveryRunner(
	runners []workerruntime.NamedScheduler,
	appConfig *settings.Config,
	infra *sharedmodules.InfraModule,
	irisSender *egress.IrisMessageSender,
	logger *slog.Logger,
	workerState *alarmWorkerRegistryState,
) ([]workerruntime.NamedScheduler, error) {
	if !alarmWorkerExecutorEnabled(appConfig, "notification_delivery") {
		logWorkerDisabled(logger, "Notification delivery outbox dispatcher disabled")

		return runners, nil
	}

	dispatcher, err := buildDeliveryOutboxDispatcher(appConfig, infra, irisSender, logger, workerState)
	if err != nil {
		return nil, fmt.Errorf("build delivery outbox dispatcher: %w", err)
	}

	return append(runners, workerruntime.NamedScheduler{
		Name:      "notification-delivery-outbox",
		Scheduler: workerState.wrap("notification_delivery", dispatcher),
	}), nil
}

func alarmWorkerExecutorEnabled(appConfig *settings.Config, workerID string) bool {
	return appConfig.AlarmWorkerProfile.Loaded.Profile.Workers[workerID].Executor.Enabled
}

func logWorkerDisabled(logger *slog.Logger, message string) {
	if logger != nil {
		logger.Info(message)
	}
}

func buildDeliveryOutboxDispatcher(
	appConfig *settings.Config,
	infra *sharedmodules.InfraModule,
	sender delivery.MessageSender,
	logger *slog.Logger,
	workerState *alarmWorkerRegistryState,
) (workerruntime.Scheduler, error) {
	if infra == nil || infra.Postgres == nil {
		return nil, errors.New("postgres is required")
	}

	worker := appConfig.AlarmWorkerProfile.Loaded.Profile.Workers["notification_delivery"]
	profile := appConfig.AlarmWorkerProfile.NotificationDelivery
	dispatcherConfig := delivery.DispatcherConfig{
		BatchSize: profile.BatchSize, MaxConcurrent: worker.Executor.ConfiguredWorkers,
		MaxRetries: profile.MaxRetries, LockTimeout: durationMS(profile.LockTimeoutMS),
		PollInterval: durationMS(profile.PollIntervalMS), RetryBackoff: durationMS(profile.RetryBackoffMS),
		CleanupAfter: durationMS(profile.CleanupAfterMS), CleanupInterval: durationMS(profile.CleanupIntervalMS),
		CleanupEnabled: profile.CleanupEnabled, StaleSendingAfter: durationMS(profile.StaleSendingAfterMS),
		StaleSendingSweepInterval: durationMS(profile.StaleSendingSweepIntervalMS),
		StaleSendingSweepLimit:    profile.StaleSendingSweepLimit,
	}
	dispatcher := delivery.NewDispatcher(
		delivery.NewOutboxRepository(infra.Postgres, logger),
		sender,
		logger,
		&dispatcherConfig,
	)
	dispatcher.SetWorkerInstrumentation(workerState.trackers["notification_delivery"], workerState.totals["notification_delivery"])

	return workerruntime.NewDeliveryOutboxDispatcherRunner(dispatcher, logger), nil
}

func buildAlarmDispatchRunner(
	ctx context.Context,
	appConfig *settings.Config,
	infra *sharedmodules.InfraModule,
	sender dispatchrun.Sender,
	logger *slog.Logger,
	workerState *alarmWorkerRegistryState,
) (workerruntime.Scheduler, error) {
	if err := dispatchrun.ValidateAlarmShortLinkConfig(parseAlarmDispatchKaringEnabled()); err != nil {
		return nil, fmt.Errorf("validate alarm dispatch short links: %w", err)
	}

	if infra == nil {
		return nil, errors.New("infra is required")
	}

	if infra.Postgres == nil {
		return nil, errors.New("postgres is required")
	}

	consumer := newAlarmDispatchConsumer(appConfig, infra, logger)
	config := alarmDispatchRunnerConfig(appConfig)

	config.WorkerTracker = workerState.trackers["alarm_dispatch"]
	config.WorkerTotals = workerState.totals["alarm_dispatch"]

	if infra.MemberCache != nil {
		config.Members = providers.ProvideMemberServiceAdapter(ctx, infra.MemberCache, logger)
	}

	wakeupWaiter, err := dispatchrun.NewWakeupWaiterWithConfig(infra.Cache, logger, dispatchrun.WakeupConfig{
		WakeupEnabled: appConfig.AlarmWorkerProfile.AlarmDispatch.WakeupEnabled,
		PollInterval:  durationMS(appConfig.AlarmWorkerProfile.AlarmDispatch.PollIntervalMS),
		BackoffMin:    durationMS(appConfig.AlarmWorkerProfile.AlarmDispatch.IdleBackoffMinMS),
		BackoffMax:    durationMS(appConfig.AlarmWorkerProfile.AlarmDispatch.IdleBackoffMaxMS),
	})
	if err != nil {
		return nil, fmt.Errorf("wakeup waiter with config: %w", err)
	}

	return dispatchrun.NewRunner(
		consumer,
		sender,
		template.NewRenderer(infra.Postgres.GetPool(), logger),
		alarmDispatchMessageStrings(infra, logger),
		wakeupWaiter,
		config,
		logger,
	), nil
}

func newAlarmDispatchConsumer(appConfig *settings.Config, infra *sharedmodules.InfraModule, logger *slog.Logger) *dispatchoutbox.Consumer {
	profile := appConfig.AlarmWorkerProfile.AlarmDispatch
	lease := durationMS(profile.LeaseMS)

	return dispatchoutbox.NewConsumer(
		dispatchoutbox.NewPgxRepository(infra.Postgres, logger),
		logger,
		dispatchoutbox.WithLease(lease),
		dispatchoutbox.WithQuarantineThreshold(durationMS(profile.QuarantineThresholdMS)),
		dispatchoutbox.WithRecoveryInterval(durationMS(profile.RecoveryIntervalMS)),
		dispatchoutbox.WithRecoveryBatchSize(profile.RecoveryBatchSize),
		dispatchoutbox.WithClaimKeyReleaser(infra.Cache),
	)
}

func alarmDispatchRunnerConfig(appConfig *settings.Config) dispatchrun.RunnerConfig {
	profile := appConfig.AlarmWorkerProfile.AlarmDispatch
	worker := appConfig.AlarmWorkerProfile.Loaded.Profile.Workers["alarm_dispatch"]

	return dispatchrun.RunnerConfig{
		KaringEnabled:     parseAlarmDispatchKaringEnabled(),
		MaxBatch:          profile.MaxBatch,
		MaxBatchesPerWake: profile.MaxBatchesPerWake,
		AttemptTimeout:    time.Duration(*worker.Executor.AttemptTimeout.Milliseconds) * time.Millisecond,
	}
}

func parseAlarmDispatchKaringEnabled() bool {
	return envutil.Bool("ALARM_DISPATCH_KARING_ENABLED", false)
}

func alarmDispatchMessageStrings(infra *sharedmodules.InfraModule, logger *slog.Logger) *messagestrings.Store {
	if infra == nil || infra.Postgres == nil {
		return nil
	}

	pool := infra.Postgres.GetPool()
	if pool == nil {
		return nil
	}

	return messagestrings.NewStore(pool, logger)
}

func buildYouTubeOutboxSender(irisSender *egress.IrisMessageSender, messageStrings *messagestrings.Store) delivery.MessageSender {
	if !envutil.Bool("YOUTUBE_OUTBOX_KARING_ENABLED", false) {
		return irisSender
	}

	return dispatchrun.NewYouTubeOutboxKaringSender(irisSender, messageStrings)
}

func buildYouTubeOutboxDispatcher(
	appConfig *settings.Config,
	infra *sharedmodules.InfraModule,
	sender delivery.MessageSender,
	logger *slog.Logger,
	workerState *alarmWorkerRegistryState,
	mode handoff.Mode,
) (workerruntime.Scheduler, error) {
	if infra == nil || infra.Postgres == nil {
		return nil, errors.New("postgres is required")
	}

	if err := validateYouTubeOutboxHandoffConfig(mode); err != nil {
		return nil, fmt.Errorf("validate youtube outbox handoff config: %w", err)
	}

	dispatcher := newYouTubeOutboxDispatcher(appConfig, infra, sender, logger)
	dispatcher.SetWorkerInstrumentation(workerState.trackers["youtube_delivery"], workerState.totals["youtube_delivery"])

	if err := configureYouTubeOutboxDispatcher(dispatcher, infra, logger, mode); err != nil {
		return nil, fmt.Errorf("configure youtube outbox dispatcher: %w", err)
	}

	return workerruntime.NewYouTubeOutboxDispatcherRunner(dispatcher, logger), nil
}

func youtubeOutboxHandoffActivation(appConfig *settings.Config, logger *slog.Logger) (handoff.Mode, bool, error) {
	mode, err := parseYouTubeOutboxHandoffMode()
	if err != nil {
		return mode, false, fmt.Errorf("parse youtube outbox handoff mode: %w", err)
	}

	enabled, err := youtubeOutboxDispatcherEnabled(appConfig, mode, logger)
	if err != nil {
		return mode, false, fmt.Errorf("youtube outbox dispatcher enabled: %w", err)
	}

	return mode, enabled, nil
}

func parseYouTubeOutboxHandoffMode() (handoff.Mode, error) {
	out, err := handoff.ParseMode(envutil.String("YOUTUBE_OUTBOX_V3_HANDOFF_MODE", "off"))
	if err != nil {
		return out, fmt.Errorf("parse mode: %w", err)
	}

	return out, nil
}

func youtubeOutboxDispatcherEnabled(appConfig *settings.Config, mode handoff.Mode, logger *slog.Logger) (bool, error) {
	if appConfig.AlarmWorkerProfile.Loaded.Profile.Workers["youtube_delivery"].Executor.Enabled {
		return true, nil
	}

	if mode != handoff.ModeOff {
		return false, fmt.Errorf("youtube outbox v3 handoff mode %q requires youtube_delivery executor.enabled=true", mode)
	}

	if logger != nil {
		logger.Info("YouTube outbox dispatcher disabled")
	}

	return false, nil
}

func validateYouTubeOutboxHandoffConfig(_ handoff.Mode) error {
	return nil
}

func newYouTubeOutboxDispatcher(
	appConfig *settings.Config,
	infra *sharedmodules.InfraModule,
	sender delivery.MessageSender,
	logger *slog.Logger,
) *youtubedispatch.Dispatcher {
	profile := appConfig.AlarmWorkerProfile.YouTubeDelivery
	worker := appConfig.AlarmWorkerProfile.Loaded.Profile.Workers["youtube_delivery"]
	dispatchConfig := dispatchstate.Config{
		BatchSize: profile.BatchSize, LockTimeout: durationMS(profile.LockTimeoutMS),
		PollInterval: durationMS(profile.PollIntervalMS), MaxRetries: profile.MaxRetries,
		RetryBackoff: durationMS(profile.RetryBackoffMS), CleanupAfter: durationMS(profile.CleanupAfterMS),
		CleanupEnabled: profile.CleanupEnabled, ReviveEnabled: profile.ReviveEnabled,
		ReviveInterval: durationMS(profile.ReviveIntervalMS), ReviveFreshnessWindow: durationMS(profile.ReviveFreshnessWindowMS),
		ClaimFreshnessWindow: durationMS(profile.ClaimFreshnessWindowMS), DeliveryParallelism: worker.Executor.ConfiguredWorkers,
		DeliverySendTimeout: durationMS(profile.DeliverySendTimeoutMS), SubscriberLookupParallelism: profile.SubscriberLookupParallelism,
		AggregateSyncInterval: durationMS(profile.AggregateSyncIntervalMS), TelemetryPollInterval: durationMS(profile.TelemetryPollIntervalMS),
		TelemetryBackfillBatch: profile.TelemetryBackfillBatch, TelemetryFlushBatch: profile.TelemetryFlushBatch,
		TelemetryRetryBackoff: durationMS(profile.TelemetryRetryBackoffMS), TelemetryRetention: durationMS(profile.TelemetryRetentionMS),
	}

	return youtubedispatch.NewDispatcher(
		infra.Postgres.GetPool(),
		infra.Cache,
		sender,
		template.NewRenderer(infra.Postgres.GetPool(), logger),
		logger,
		&dispatchConfig,
	)
}

func durationMS(milliseconds int64) time.Duration {
	return time.Duration(milliseconds) * time.Millisecond
}

func configureYouTubeOutboxDispatcher(
	dispatcher *youtubedispatch.Dispatcher,
	infra *sharedmodules.InfraModule,
	logger *slog.Logger,
	mode handoff.Mode,
) error {
	if mode == handoff.ModeOff {
		return nil
	}

	publisher := queue.NewPublisher(
		infra.Cache,
		logger,
		queue.WithOutbox(dispatchoutbox.NewPgxRepository(infra.Postgres, logger)),
	)

	if err := dispatcher.ConfigureHandoff(mode, youtubeOutboxDispatchPublisher{publisher: publisher}); err != nil {
		return fmt.Errorf("configure handoff: %w", err)
	}

	return nil
}
