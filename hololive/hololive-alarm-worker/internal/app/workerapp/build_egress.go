package workerapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/park285/iris-client-go/v3/iris"

	"github.com/kapu/hololive-alarm-worker/internal/egress"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch"
	"github.com/kapu/hololive-alarm-worker/internal/service/dispatchrun"
	"github.com/kapu/hololive-alarm-worker/internal/service/workerruntime"
	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/config/settings/alarmworker"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/service/kakaoroom"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

func buildNotificationEgress(
	ctx context.Context,
	appConfig *alarmworker.RuntimeConfig,
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

	irisClient, err := providers.ProvideIrisClient(
		&appConfig.Iris,
		logger,
		iris.WithBaseURL(appConfig.Iris.BaseURL),
		iris.WithBotToken(appConfig.Iris.BotToken),
	)
	if err != nil {
		return nil, fmt.Errorf("init alarm-worker notification egress iris client: %w", err)
	}

	rooms := kakaoroom.New(infra.Postgres.GetPool(), kakaoroom.NewIrisLister(irisClient), logger)
	irisSender := buildNotificationSender(irisClient, appConfig.Bot.MarkdownReplies, rooms)

	messageStrings, err := loadEgressMessageStrings(ctx, infra, logger)
	if err != nil {
		return nil, fmt.Errorf("load egress message strings: %w", err)
	}

	runners, err := buildEgressRunners(ctx, appConfig, infra, irisSender, messageStrings, logger, workerState)
	if err != nil {
		return nil, fmt.Errorf("build egress runners: %w", err)
	}

	return workerruntime.NewNotificationEgressRunner(runners, logger), nil
}

func buildNotificationSender(client egress.IrisClient, markdownReplies bool, rooms egress.OpenChat) *egress.IrisMessageSender {
	// 방 정본은 오픈채팅 Markdown 판정에만 쓴다. alarm-worker는 Karing template을 보내지 않는다
	// (DEC-20260926-hololive-karing-egress-disposition).
	return egress.NewIrisMessageSender(
		client,
		egress.WithMarkdownReplies(markdownReplies),
		egress.WithMarkdownRoomChat(rooms),
	)
}

func buildEgressRunners(
	ctx context.Context,
	appConfig *alarmworker.RuntimeConfig,
	infra *sharedmodules.InfraModule,
	irisSender *egress.IrisMessageSender,
	messageStrings *messagestrings.Store,
	logger *slog.Logger,
	workerState *alarmWorkerRegistryState,
) ([]workerruntime.NamedScheduler, error) {
	runners, err := appendAlarmDispatchRunner(ctx, nil, appConfig.Config, infra, irisSender, messageStrings, logger, workerState)
	if err != nil {
		return nil, fmt.Errorf("append alarm dispatch runner: %w", err)
	}

	runners = append(runners, workerruntime.NamedScheduler{
		Name:      "alarm-dispatch-maintenance",
		Scheduler: dispatchrun.NewMaintenanceRunner(infra, appConfig.DispatchRetention, logger),
	})

	// v1 YouTube 알림은 v3 ledger로 넘기지 않는 정본 파이프라인이다(DEC-20260926-hololive-outbox-v3-convergence).
	if alarmWorkerExecutorEnabled(appConfig.Config, "youtube_delivery") {
		dispatcher, buildErr := buildYouTubeOutboxDispatcher(appConfig.Config, infra, irisSender, messageStrings, logger, workerState)
		if buildErr != nil {
			return nil, fmt.Errorf("build youtube outbox dispatcher: %w", buildErr)
		}

		runners = append(runners, workerruntime.NamedScheduler{
			Name:      "youtube-outbox",
			Scheduler: workerState.wrap("youtube_delivery", dispatcher),
		})
	} else {
		logWorkerDisabled(logger, "YouTube outbox dispatcher disabled")
	}

	runners, err = appendNotificationDeliveryRunner(runners, appConfig.Config, infra, irisSender, logger, workerState)
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
	messageStrings *messagestrings.Store,
	logger *slog.Logger,
	workerState *alarmWorkerRegistryState,
) ([]workerruntime.NamedScheduler, error) {
	if !alarmWorkerExecutorEnabled(appConfig, "alarm_dispatch") {
		logWorkerDisabled(logger, "Alarm dispatch consumer disabled")

		return runners, nil
	}

	runner, err := buildAlarmDispatchRunner(ctx, appConfig, infra, irisSender, messageStrings, logger, workerState)
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

	dispatcherConfig := notificationDeliveryDispatcherConfig(appConfig)

	dispatcher, err := delivery.NewDispatcher(
		delivery.NewOutboxRepository(infra.Postgres, logger),
		sender,
		logger,
		&dispatcherConfig,
	)
	if err != nil {
		return nil, fmt.Errorf("new delivery dispatcher: %w", err)
	}

	dispatcher.SetWorkerInstrumentation(workerState.trackers["notification_delivery"], workerState.totals["notification_delivery"])

	return workerruntime.NewDeliveryOutboxDispatcherRunner(dispatcher, logger), nil
}

// notificationDeliveryDispatcherConfig는 worker profile의 notification_delivery 항목을 그대로 옮긴다. 시도 시간은
// executor의 fixed attempt_timeout이며, profile 스키마가 fixed 모드와 값을 요구하므로 dispatcher 기본값을 쓰지 않는다.
func notificationDeliveryDispatcherConfig(appConfig *settings.Config) delivery.DispatcherConfig {
	worker := appConfig.AlarmWorkerProfile.Loaded.Profile.Workers["notification_delivery"]
	profile := appConfig.AlarmWorkerProfile.NotificationDelivery

	return delivery.DispatcherConfig{
		AttemptTimeout:            time.Duration(*worker.Executor.AttemptTimeout.Milliseconds) * time.Millisecond,
		BatchSize:                 profile.BatchSize,
		MaxConcurrent:             worker.Executor.ConfiguredWorkers,
		MaxRetries:                profile.MaxRetries,
		PollInterval:              durationMS(profile.PollIntervalMS),
		RetryBackoff:              durationMS(profile.RetryBackoffMS),
		CleanupAfter:              durationMS(profile.CleanupAfterMS),
		CleanupInterval:           durationMS(profile.CleanupIntervalMS),
		CleanupEnabled:            profile.CleanupEnabled,
		StaleSendingAfter:         durationMS(profile.StaleSendingAfterMS),
		StaleSendingSweepInterval: durationMS(profile.StaleSendingSweepIntervalMS),
		StaleSendingSweepLimit:    profile.StaleSendingSweepLimit,
	}
}

func buildAlarmDispatchRunner(
	ctx context.Context,
	appConfig *settings.Config,
	infra *sharedmodules.InfraModule,
	sender dispatchrun.Sender,
	messageStrings *messagestrings.Store,
	logger *slog.Logger,
	workerState *alarmWorkerRegistryState,
) (workerruntime.Scheduler, error) {
	if err := dispatchrun.ValidateAlarmShortLinkConfig(appConfig.Notification.AlarmShortLinkBaseURL); err != nil {
		return nil, fmt.Errorf("validate alarm dispatch short links: %w", err)
	}

	if infra == nil {
		return nil, errors.New("infra is required")
	}

	if infra.Postgres == nil {
		return nil, errors.New("postgres is required")
	}

	consumer, err := newAlarmDispatchConsumer(appConfig, infra, logger)
	if err != nil {
		return nil, fmt.Errorf("new alarm dispatch consumer: %w", err)
	}

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
		messageStrings,
		wakeupWaiter,
		config,
		logger,
	), nil
}

func newAlarmDispatchConsumer(appConfig *settings.Config, infra *sharedmodules.InfraModule, logger *slog.Logger) (*dispatchoutbox.Consumer, error) {
	profile := appConfig.AlarmWorkerProfile.AlarmDispatch
	lease := durationMS(profile.LeaseMS)

	consumer, err := dispatchoutbox.NewConsumer(
		dispatchoutbox.NewPgxRepository(infra.Postgres, logger),
		infra.Cache,
		logger,
		dispatchoutbox.WithLease(lease),
		dispatchoutbox.WithQuarantineThreshold(durationMS(profile.QuarantineThresholdMS)),
		dispatchoutbox.WithRecoveryInterval(durationMS(profile.RecoveryIntervalMS)),
		dispatchoutbox.WithRecoveryBatchSize(profile.RecoveryBatchSize),
	)
	if err != nil {
		return nil, fmt.Errorf("new dispatch outbox consumer: %w", err)
	}

	return consumer, nil
}

func alarmDispatchRunnerConfig(appConfig *settings.Config) dispatchrun.RunnerConfig {
	profile := appConfig.AlarmWorkerProfile.AlarmDispatch
	worker := appConfig.AlarmWorkerProfile.Loaded.Profile.Workers["alarm_dispatch"]

	return dispatchrun.RunnerConfig{
		ShortLinkBaseURL:  appConfig.Notification.AlarmShortLinkBaseURL,
		MaxBatch:          profile.MaxBatch,
		MaxBatchesPerWake: profile.MaxBatchesPerWake,
		AttemptTimeout:    time.Duration(*worker.Executor.AttemptTimeout.Milliseconds) * time.Millisecond,
	}
}

// loadEgressMessageStrings는 알림 발송 runner들이 함께 쓰는 message_strings를 기동 때 한 번 적재하고
// 발송 계약 key를 검증한다. 실패하면 기동을 실패시킨다(DEC-20260926-hololive-message-strings-startup-validation).
func loadEgressMessageStrings(ctx context.Context, infra *sharedmodules.InfraModule, logger *slog.Logger) (*messagestrings.Store, error) {
	if infra == nil || infra.Postgres == nil {
		return nil, errors.New("postgres is required")
	}

	pool := infra.Postgres.GetPool()
	if pool == nil {
		return nil, errors.New("postgres pool is required")
	}

	store := messagestrings.NewStore(pool, logger)
	if err := store.Load(ctx); err != nil {
		return nil, fmt.Errorf("load message strings: %w", err)
	}

	if err := store.Validate(messagestrings.AlarmWorkerEgressRequirements()); err != nil {
		return nil, fmt.Errorf("validate message strings: %w", err)
	}

	return store, nil
}

func buildYouTubeOutboxDispatcher(
	appConfig *settings.Config,
	infra *sharedmodules.InfraModule,
	sender delivery.MessageSender,
	messageStrings *messagestrings.Store,
	logger *slog.Logger,
	workerState *alarmWorkerRegistryState,
) (workerruntime.Scheduler, error) {
	if infra == nil || infra.Postgres == nil {
		return nil, errors.New("postgres is required")
	}

	dispatcher, err := newYouTubeOutboxDispatcher(appConfig, infra, sender, messageStrings, logger)
	if err != nil {
		return nil, fmt.Errorf("create youtube outbox dispatcher: %w", err)
	}

	dispatcher.SetWorkerInstrumentation(workerState.trackers["youtube_delivery"], workerState.totals["youtube_delivery"])

	return workerruntime.NewYouTubeOutboxDispatcherRunner(dispatcher, logger), nil
}

func newYouTubeOutboxDispatcher(
	appConfig *settings.Config,
	infra *sharedmodules.InfraModule,
	sender delivery.MessageSender,
	messageStrings *messagestrings.Store,
	logger *slog.Logger,
) (*youtubedispatch.Dispatcher, error) {
	if appConfig == nil {
		return nil, errors.New("youtube outbox config is required")
	}

	if infra == nil || infra.Postgres == nil || infra.Postgres.GetPool() == nil {
		return nil, errors.New("youtube outbox postgres pool is required")
	}

	dispatchConfig, err := youtubeDispatchConfig(appConfig)
	if err != nil {
		return nil, err
	}

	pool := infra.Postgres.GetPool()

	dispatcher, err := youtubedispatch.NewDispatcher(youtubedispatch.Dependencies{
		DB: pool, Cache: infra.Cache, Sender: sender,
		Renderer: template.NewRenderer(pool, logger), MessageStrings: messageStrings,
		// 표시명은 PostgreSQL 정본(members → alarms.member_name)에서 읽어 Valkey 장애가 알림 이름과 발송을 막지 않게 한다.
		MemberNames: sharedalarm.NewRepository(infra.Postgres, logger),
	}, logger, &dispatchConfig)
	if err != nil {
		return nil, fmt.Errorf("initialize youtube outbox dispatcher dependencies: %w", err)
	}

	return dispatcher, nil
}

// youtubeDispatchConfig는 worker profile의 youtube_delivery 항목을 그대로 옮긴다. 생성자가 기본값을 채우지 않으므로
// 모든 필드를 profile에서 받는다.
func youtubeDispatchConfig(appConfig *settings.Config) (dispatchstate.Config, error) {
	profile := appConfig.AlarmWorkerProfile.YouTubeDelivery
	worker := appConfig.AlarmWorkerProfile.Loaded.Profile.Workers["youtube_delivery"]
	attemptTimeout := time.Duration(*worker.Executor.AttemptTimeout.Milliseconds) * time.Millisecond
	// 공통 executor 예산과 보존 중인 service 설정은 같은 provider 호출을 제한합니다.
	if durationMS(profile.DeliverySendTimeoutMS) != attemptTimeout {
		return dispatchstate.Config{}, errors.New("youtube delivery send timeout must match executor attempt timeout")
	}

	return dispatchstate.Config{
		BatchSize:                   profile.BatchSize,
		LockTimeout:                 durationMS(profile.LockTimeoutMS),
		PollInterval:                durationMS(profile.PollIntervalMS),
		MaxRetries:                  profile.MaxRetries,
		RetryBackoff:                durationMS(profile.RetryBackoffMS),
		CleanupAfter:                durationMS(profile.CleanupAfterMS),
		CleanupEnabled:              profile.CleanupEnabled,
		ReviveEnabled:               profile.ReviveEnabled,
		ReviveInterval:              durationMS(profile.ReviveIntervalMS),
		ReviveFreshnessWindow:       durationMS(profile.ReviveFreshnessWindowMS),
		ClaimFreshnessWindow:        durationMS(profile.ClaimFreshnessWindowMS),
		DeliveryParallelism:         worker.Executor.ConfiguredWorkers,
		DeliverySendTimeout:         attemptTimeout,
		SubscriberLookupParallelism: profile.SubscriberLookupParallelism,
		AggregateSyncInterval:       durationMS(profile.AggregateSyncIntervalMS),
		TelemetryPollInterval:       durationMS(profile.TelemetryPollIntervalMS),
		TelemetryFlushBatch:         profile.TelemetryFlushBatch,
		TelemetryRetryBackoff:       durationMS(profile.TelemetryRetryBackoffMS),
		TelemetryRetention:          durationMS(profile.TelemetryRetentionMS),
	}, nil
}

func durationMS(milliseconds int64) time.Duration {
	return time.Duration(milliseconds) * time.Millisecond
}
