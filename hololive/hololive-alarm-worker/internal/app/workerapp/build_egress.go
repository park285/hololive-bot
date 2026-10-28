package workerapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	workerconfig "github.com/kapu/hololive-alarm-worker/internal/config"
	"github.com/kapu/hololive-alarm-worker/internal/egress"
	"github.com/kapu/hololive-alarm-worker/internal/egress/alarmdispatch"
	"github.com/kapu/hololive-alarm-worker/internal/egress/handoffcheck"
	"github.com/kapu/hololive-alarm-worker/internal/egress/notificationdelivery"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/alarmstore"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-alarm-worker/internal/service/workerruntime"
	irisproviders "github.com/kapu/hololive-shared/pkg/providers/iris"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/service/kakaoroom"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

func buildNotificationEgress(
	ctx context.Context,
	appConfig *workerconfig.RuntimeConfig,
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

	irisClient, err := irisproviders.ProvideIrisClient(ctx, &appConfig.Iris, logger)
	if err != nil {
		return nil, fmt.Errorf("init alarm-worker notification egress iris client: %w", err)
	}

	rooms := kakaoroom.New(infra.Postgres.GetPool(), kakaoroom.NewIrisLister(irisClient), logger)
	irisSender := buildNotificationSender(irisClient, appConfig.MarkdownReplies, rooms)

	messageStrings, err := loadEgressMessageStrings(ctx, infra, logger)
	if err != nil {
		return nil, fmt.Errorf("load egress message strings: %w", err)
	}

	runners, err := buildEgressRunners(appConfig, infra, irisSender, irisClient, messageStrings, logger, workerState)
	if err != nil {
		return nil, fmt.Errorf("build egress runners: %w", err)
	}

	return workerruntime.NewNotificationEgressRunner(runners, logger), nil
}

func buildNotificationSender(client egress.IrisClient, markdownReplies bool, rooms egress.OpenChat) *egress.IrisMessageSender {
	// 방 정본은 오픈채팅 Markdown 판정에만 쓴다. alarm-worker는 Karing template을 보내지 않는다.
	return egress.NewIrisMessageSender(
		client,
		egress.WithMarkdownReplies(markdownReplies),
		egress.WithMarkdownRoomChat(rooms),
	)
}

func buildEgressRunners(
	appConfig *workerconfig.RuntimeConfig,
	infra *sharedmodules.InfraModule,
	irisSender *egress.IrisMessageSender,
	statusClient handoffcheck.ReplyStatusClient,
	messageStrings *messagestrings.Store,
	logger *slog.Logger,
	workerState *alarmWorkerRegistryState,
) ([]workerruntime.NamedScheduler, error) {
	runners, err := appendAlarmDispatchRunner(nil, appConfig, infra, irisSender, statusClient, messageStrings, logger, workerState)
	if err != nil {
		return nil, fmt.Errorf("append alarm dispatch runner: %w", err)
	}

	runners = append(runners,
		workerruntime.NamedScheduler{
			Name:      "alarm-dispatch-maintenance",
			Scheduler: alarmdispatch.NewMaintenanceRunner(infra, appConfig.DispatchRetention, logger),
		},
		workerruntime.NamedScheduler{
			Name:      "alarm-dispatch-backlog-observer",
			Scheduler: alarmdispatch.NewBacklogObserver(infra, logger),
		},
	)

	// v1 YouTube 알림은 v3 ledger로 넘기지 않는 정본 파이프라인이다.
	runners, err = appendYouTubeDeliveryRunner(runners, appConfig, infra, irisSender, statusClient, messageStrings, logger, workerState)
	if err != nil {
		return nil, fmt.Errorf("append youtube delivery runner: %w", err)
	}

	runners, err = appendNotificationDeliveryRunner(runners, appConfig, infra, irisSender, statusClient, logger, workerState)
	if err != nil {
		return nil, fmt.Errorf("append notification delivery runner: %w", err)
	}

	return runners, nil
}

// appendYouTubeDeliveryRunner는 youtube_delivery가 활성일 때 발송 runner와 고정 요청 Iris handoff reconciler를 함께
// 등록한다.
func appendYouTubeDeliveryRunner(
	runners []workerruntime.NamedScheduler,
	appConfig *workerconfig.RuntimeConfig,
	infra *sharedmodules.InfraModule,
	irisSender *egress.IrisMessageSender,
	statusClient handoffcheck.ReplyStatusClient,
	messageStrings *messagestrings.Store,
	logger *slog.Logger,
	workerState *alarmWorkerRegistryState,
) ([]workerruntime.NamedScheduler, error) {
	if !alarmWorkerExecutorEnabled(appConfig, workerconfig.WorkerYouTubeDelivery) {
		logWorkerDisabled(logger, "YouTube outbox dispatcher disabled")

		return runners, nil
	}

	dispatcher, err := buildYouTubeOutboxDispatcher(appConfig, infra, irisSender, messageStrings, logger, workerState)
	if err != nil {
		return nil, fmt.Errorf("build youtube outbox dispatcher: %w", err)
	}

	dispatchConfig := youtubeDispatchConfig(appConfig)

	reconciler, err := youtubedispatch.NewHandoffReconciler(infra.Postgres.GetPool(), statusClient, logger, &dispatchConfig)
	if err != nil {
		return nil, fmt.Errorf("build youtube delivery handoff reconciler: %w", err)
	}

	return append(runners,
		workerruntime.NamedScheduler{
			Name:      "youtube-outbox",
			Scheduler: workerState.wrap(workerconfig.WorkerYouTubeDelivery, dispatcher),
		},
		workerruntime.NamedScheduler{
			Name:      "youtube-delivery-handoff-reconciler",
			Scheduler: reconciler,
		},
	), nil
}

// appendAlarmDispatchRunner는 alarm-dispatch가 활성일 때 발송 runner와 Iris handoff reconciler를 함께 등록한다.
func appendAlarmDispatchRunner(
	runners []workerruntime.NamedScheduler,
	appConfig *workerconfig.RuntimeConfig,
	infra *sharedmodules.InfraModule,
	irisSender *egress.IrisMessageSender,
	statusClient handoffcheck.ReplyStatusClient,
	messageStrings *messagestrings.Store,
	logger *slog.Logger,
	workerState *alarmWorkerRegistryState,
) ([]workerruntime.NamedScheduler, error) {
	if !alarmWorkerExecutorEnabled(appConfig, workerconfig.WorkerAlarmDispatch) {
		logWorkerDisabled(logger, "Alarm dispatch consumer disabled")

		return runners, nil
	}

	runner, err := buildAlarmDispatchRunner(appConfig, infra, irisSender, messageStrings, logger, workerState)
	if err != nil {
		return nil, fmt.Errorf("build alarm dispatch runner: %w", err)
	}

	reconciler, err := buildAlarmDispatchHandoffReconciler(appConfig, infra, statusClient, logger)
	if err != nil {
		return nil, fmt.Errorf("build alarm dispatch handoff reconciler: %w", err)
	}

	return append(runners,
		workerruntime.NamedScheduler{
			Name:      "alarm-dispatch",
			Scheduler: workerState.wrap(workerconfig.WorkerAlarmDispatch, runner),
		},
		workerruntime.NamedScheduler{
			Name:      "alarm-dispatch-handoff-reconciler",
			Scheduler: reconciler,
		},
	), nil
}

// buildAlarmDispatchHandoffReconciler의 지평은 alarm_dispatch executor attempt timeout이고 idle 주기는 alarm_dispatch
// poll 주기다. DLQ 뒤 dedup claim 키는 발송 runner와 같은 cache로 해제한다.
func buildAlarmDispatchHandoffReconciler(
	appConfig *workerconfig.RuntimeConfig,
	infra *sharedmodules.InfraModule,
	statusClient handoffcheck.ReplyStatusClient,
	logger *slog.Logger,
) (*alarmdispatch.HandoffReconciler, error) {
	repository, err := dispatchoutbox.NewPgxRepository(infra.Postgres, logger)
	if err != nil {
		return nil, fmt.Errorf("new alarm dispatch handoff repository: %w", err)
	}

	consumer, err := dispatchoutbox.NewHandoffConsumer(repository, infra.Cache)
	if err != nil {
		return nil, fmt.Errorf("new alarm dispatch handoff consumer: %w", err)
	}

	worker := appConfig.AlarmWorkerProfile.Loaded.Profile.Workers[workerconfig.WorkerAlarmDispatch]

	reconciler, err := alarmdispatch.NewHandoffReconciler(consumer, statusClient, alarmdispatch.HandoffReconcilerConfig{
		Horizon:      workercontract.Milliseconds(*worker.Executor.AttemptTimeout.Milliseconds),
		PollInterval: workercontract.Milliseconds(appConfig.AlarmWorkerProfile.AlarmDispatch.PollIntervalMS),
	}, logger)
	if err != nil {
		return nil, fmt.Errorf("new alarm dispatch handoff reconciler: %w", err)
	}

	return reconciler, nil
}

// appendNotificationDeliveryRunner는 notification delivery가 활성일 때 발송 dispatcher와 Iris handoff reconciler를 함께
// 등록한다.
func appendNotificationDeliveryRunner(
	runners []workerruntime.NamedScheduler,
	appConfig *workerconfig.RuntimeConfig,
	infra *sharedmodules.InfraModule,
	irisSender *egress.IrisMessageSender,
	statusClient handoffcheck.ReplyStatusClient,
	logger *slog.Logger,
	workerState *alarmWorkerRegistryState,
) ([]workerruntime.NamedScheduler, error) {
	if !alarmWorkerExecutorEnabled(appConfig, workerconfig.WorkerNotificationDelivery) {
		logWorkerDisabled(logger, "Notification delivery outbox dispatcher disabled")

		return runners, nil
	}

	dispatcher, err := buildDeliveryOutboxDispatcher(appConfig, infra, irisSender, logger, workerState)
	if err != nil {
		return nil, fmt.Errorf("build delivery outbox dispatcher: %w", err)
	}

	reconciler, err := buildNotificationDeliveryHandoffReconciler(appConfig, infra, statusClient, logger)
	if err != nil {
		return nil, fmt.Errorf("build notification delivery handoff reconciler: %w", err)
	}

	return append(runners,
		workerruntime.NamedScheduler{
			Name:      "notification-delivery-outbox",
			Scheduler: workerState.wrap(workerconfig.WorkerNotificationDelivery, dispatcher),
		},
		workerruntime.NamedScheduler{
			Name:      "notification-delivery-handoff-reconciler",
			Scheduler: reconciler,
		},
	), nil
}

// buildNotificationDeliveryHandoffReconciler의 지평은 notification_delivery executor attempt timeout이고, idle 주기와
// failed 재발급의 시도 예산·지연은 dispatcher와 같은 notification_delivery profile 값이다. PostgreSQL 확인은 먼저 만든
// dispatcher가 한다.
func buildNotificationDeliveryHandoffReconciler(
	appConfig *workerconfig.RuntimeConfig,
	infra *sharedmodules.InfraModule,
	statusClient handoffcheck.ReplyStatusClient,
	logger *slog.Logger,
) (*notificationdelivery.HandoffReconciler, error) {
	dispatcherConfig := notificationDeliveryDispatcherConfig(appConfig)

	reconciler, err := notificationdelivery.NewHandoffReconciler(
		notificationdelivery.NewStore(infra.Postgres.GetPool()),
		statusClient,
		notificationdelivery.HandoffReconcilerConfig{
			Horizon:      dispatcherConfig.AttemptTimeout,
			PollInterval: dispatcherConfig.PollInterval,
			MaxRetries:   dispatcherConfig.MaxRetries,
			RetryBackoff: dispatcherConfig.RetryBackoff,
		},
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("new notification delivery handoff reconciler: %w", err)
	}

	return reconciler, nil
}

func alarmWorkerExecutorEnabled(appConfig *workerconfig.RuntimeConfig, workerID string) bool {
	return appConfig.AlarmWorkerProfile.Loaded.Profile.Workers[workerID].Executor.Enabled
}

func logWorkerDisabled(logger *slog.Logger, message string) {
	if logger != nil {
		logger.Info(message)
	}
}

func buildDeliveryOutboxDispatcher(
	appConfig *workerconfig.RuntimeConfig,
	infra *sharedmodules.InfraModule,
	sender delivery.PreparedMessageSender,
	logger *slog.Logger,
	workerState *alarmWorkerRegistryState,
) (workerruntime.Scheduler, error) {
	if infra == nil || infra.Postgres == nil {
		return nil, errors.New("postgres is required")
	}

	dispatcherConfig := notificationDeliveryDispatcherConfig(appConfig)

	dispatcher, err := notificationdelivery.NewDispatcher(
		notificationdelivery.NewStore(infra.Postgres.GetPool()),
		sender,
		logger,
		&dispatcherConfig,
	)
	if err != nil {
		return nil, fmt.Errorf("new delivery dispatcher: %w", err)
	}

	dispatcher.SetWorkerInstrumentation(workerState.trackers[workerconfig.WorkerNotificationDelivery], workerState.totals[workerconfig.WorkerNotificationDelivery])

	return workerruntime.NewDeliveryOutboxDispatcherRunner(dispatcher, logger), nil
}

// notificationDeliveryDispatcherConfig는 worker profile의 notification_delivery 항목을 그대로 옮긴다. 시도 시간은
// executor의 fixed attempt_timeout이며, profile 스키마가 fixed 모드와 값을 요구하므로 dispatcher 기본값을 쓰지 않는다.
func notificationDeliveryDispatcherConfig(appConfig *workerconfig.RuntimeConfig) notificationdelivery.DispatcherConfig {
	worker := appConfig.AlarmWorkerProfile.Loaded.Profile.Workers[workerconfig.WorkerNotificationDelivery]
	profile := appConfig.AlarmWorkerProfile.NotificationDelivery

	return notificationdelivery.DispatcherConfig{
		AttemptTimeout:            workercontract.Milliseconds(*worker.Executor.AttemptTimeout.Milliseconds),
		BatchSize:                 profile.BatchSize,
		MaxConcurrent:             worker.Executor.ConfiguredWorkers,
		MaxRetries:                profile.MaxRetries,
		PollInterval:              workercontract.Milliseconds(profile.PollIntervalMS),
		RetryBackoff:              workercontract.Milliseconds(profile.RetryBackoffMS),
		CleanupAfter:              workercontract.Milliseconds(profile.CleanupAfterMS),
		CleanupInterval:           workercontract.Milliseconds(profile.CleanupIntervalMS),
		CleanupEnabled:            profile.CleanupEnabled,
		StaleSendingAfter:         workercontract.Milliseconds(profile.StaleSendingAfterMS),
		StaleSendingSweepInterval: workercontract.Milliseconds(profile.StaleSendingSweepIntervalMS),
		StaleSendingSweepLimit:    profile.StaleSendingSweepLimit,
	}
}

func buildAlarmDispatchRunner(
	appConfig *workerconfig.RuntimeConfig,
	infra *sharedmodules.InfraModule,
	sender delivery.PreparedMessageSender,
	messageStrings *messagestrings.Store,
	logger *slog.Logger,
	workerState *alarmWorkerRegistryState,
) (workerruntime.Scheduler, error) {
	if err := alarmdispatch.ValidateAlarmShortLinkConfig(appConfig.Notification.AlarmShortLinkBaseURL); err != nil {
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

	config.WorkerTracker = workerState.trackers[workerconfig.WorkerAlarmDispatch]
	config.WorkerTotals = workerState.totals[workerconfig.WorkerAlarmDispatch]

	if infra.MemberCache != nil {
		config.Members = infra.MemberCache
	}

	wakeupWaiter, err := alarmdispatch.NewWakeupWaiterWithConfig(infra.Cache, logger, alarmdispatch.WakeupConfig{
		WakeupEnabled: appConfig.AlarmWorkerProfile.AlarmDispatch.WakeupEnabled,
		PollInterval:  workercontract.Milliseconds(appConfig.AlarmWorkerProfile.AlarmDispatch.PollIntervalMS),
		BackoffMin:    workercontract.Milliseconds(appConfig.AlarmWorkerProfile.AlarmDispatch.IdleBackoffMinMS),
		BackoffMax:    workercontract.Milliseconds(appConfig.AlarmWorkerProfile.AlarmDispatch.IdleBackoffMaxMS),
	})
	if err != nil {
		return nil, fmt.Errorf("wakeup waiter with config: %w", err)
	}

	return alarmdispatch.NewRunner(
		consumer,
		sender,
		template.NewRenderer(infra.Postgres.GetPool(), logger),
		messageStrings,
		wakeupWaiter,
		config,
		logger,
	), nil
}

func newAlarmDispatchConsumer(appConfig *workerconfig.RuntimeConfig, infra *sharedmodules.InfraModule, logger *slog.Logger) (*dispatchoutbox.Consumer, error) {
	profile := appConfig.AlarmWorkerProfile.AlarmDispatch
	lease := workercontract.Milliseconds(profile.LeaseMS)

	repository, err := dispatchoutbox.NewPgxRepository(infra.Postgres, logger)
	if err != nil {
		return nil, fmt.Errorf("new dispatch outbox repository: %w", err)
	}

	consumer, err := dispatchoutbox.NewConsumer(
		repository,
		infra.Cache,
		logger,
		dispatchoutbox.WithLease(lease),
		dispatchoutbox.WithQuarantineThreshold(workercontract.Milliseconds(profile.QuarantineThresholdMS)),
		dispatchoutbox.WithRecoveryInterval(workercontract.Milliseconds(profile.RecoveryIntervalMS)),
		dispatchoutbox.WithRecoveryBatchSize(profile.RecoveryBatchSize),
	)
	if err != nil {
		return nil, fmt.Errorf("new dispatch outbox consumer: %w", err)
	}

	return consumer, nil
}

func alarmDispatchRunnerConfig(appConfig *workerconfig.RuntimeConfig) alarmdispatch.RunnerConfig {
	profile := appConfig.AlarmWorkerProfile.AlarmDispatch
	worker := appConfig.AlarmWorkerProfile.Loaded.Profile.Workers[workerconfig.WorkerAlarmDispatch]

	return alarmdispatch.RunnerConfig{
		ShortLinkBaseURL:  appConfig.Notification.AlarmShortLinkBaseURL,
		MaxBatch:          profile.MaxBatch,
		MaxBatchesPerWake: profile.MaxBatchesPerWake,
		AttemptTimeout:    workercontract.Milliseconds(*worker.Executor.AttemptTimeout.Milliseconds),
	}
}

// loadEgressMessageStrings는 알림 발송 runner들이 함께 쓰는 message_strings를 기동 때 한 번 적재하고
// 발송 계약 key를 검증한다. 실패하면 기동을 실패시킨다.
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
	appConfig *workerconfig.RuntimeConfig,
	infra *sharedmodules.InfraModule,
	sender delivery.PreparedMessageSender,
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

	dispatcher.SetWorkerInstrumentation(workerState.trackers[workerconfig.WorkerYouTubeDelivery], workerState.totals[workerconfig.WorkerYouTubeDelivery])

	return workerruntime.NewYouTubeOutboxDispatcherRunner(dispatcher, logger), nil
}

func newYouTubeOutboxDispatcher(
	appConfig *workerconfig.RuntimeConfig,
	infra *sharedmodules.InfraModule,
	sender delivery.PreparedMessageSender,
	messageStrings *messagestrings.Store,
	logger *slog.Logger,
) (*youtubedispatch.Dispatcher, error) {
	if appConfig == nil {
		return nil, errors.New("youtube outbox config is required")
	}

	if infra == nil || infra.Postgres == nil || infra.Postgres.GetPool() == nil {
		return nil, errors.New("youtube outbox postgres pool is required")
	}

	dispatchConfig := youtubeDispatchConfig(appConfig)

	pool := infra.Postgres.GetPool()

	dispatcher, err := youtubedispatch.NewDispatcher(youtubedispatch.Dependencies{
		DB: pool, Cache: infra.Cache, Sender: sender,
		Renderer: template.NewRenderer(pool, logger), MessageStrings: messageStrings,
		// 표시명은 PostgreSQL members 정본에서 읽어 Valkey 장애가 알림 이름과 발송을 막지 않게 한다.
		MemberNames: alarmstore.NewRepository(infra.Postgres, logger),
	}, logger, &dispatchConfig)
	if err != nil {
		return nil, fmt.Errorf("initialize youtube outbox dispatcher dependencies: %w", err)
	}

	return dispatcher, nil
}

// youtubeDispatchConfig는 worker profile의 youtube_delivery 항목을 그대로 옮긴다. 생성자가 기본값을 채우지 않으므로
// 모든 필드를 profile에서 받는다.
func youtubeDispatchConfig(appConfig *workerconfig.RuntimeConfig) youtubedispatch.Config {
	profile := appConfig.AlarmWorkerProfile.YouTubeDelivery
	worker := appConfig.AlarmWorkerProfile.Loaded.Profile.Workers[workerconfig.WorkerYouTubeDelivery]
	attemptTimeout, _ := worker.Executor.AttemptTimeout.FixedDuration()
	// 신선도는 delivery 생성 시각이 아니라 부모 outbox.created_at에서 계산한다.
	freshness, _ := worker.Queue.MaxAge.FixedDuration()

	return youtubedispatch.Config{
		BatchSize:                   profile.BatchSize,
		LockTimeout:                 workercontract.Milliseconds(profile.LockTimeoutMS),
		PollInterval:                workercontract.Milliseconds(profile.PollIntervalMS),
		MaxRetries:                  profile.MaxRetries,
		RetryBackoff:                workercontract.Milliseconds(profile.RetryBackoffMS),
		CleanupAfter:                workercontract.Milliseconds(profile.CleanupAfterMS),
		CleanupEnabled:              profile.CleanupEnabled,
		ReviveEnabled:               profile.ReviveEnabled,
		ReviveInterval:              workercontract.Milliseconds(profile.ReviveIntervalMS),
		ReviveFreshnessWindow:       workercontract.Milliseconds(profile.ReviveFreshnessWindowMS),
		ClaimFreshnessWindow:        freshness,
		DeliveryParallelism:         worker.Executor.ConfiguredWorkers,
		DeliverySendTimeout:         attemptTimeout,
		SubscriberLookupParallelism: profile.SubscriberLookupParallelism,
		AggregateSyncInterval:       workercontract.Milliseconds(profile.AggregateSyncIntervalMS),
		TelemetryPollInterval:       workercontract.Milliseconds(profile.TelemetryPollIntervalMS),
		TelemetryFlushBatch:         profile.TelemetryFlushBatch,
		TelemetryRetryBackoff:       workercontract.Milliseconds(profile.TelemetryRetryBackoffMS),
		TelemetryRetention:          workercontract.Milliseconds(profile.TelemetryRetentionMS),
	}
}
