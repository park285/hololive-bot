package workerapp

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	youtubeoutbox "github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	"github.com/park285/iris-client-go/iris"

	"github.com/kapu/hololive-alarm-worker/internal/egress"
)

func buildNotificationEgress(
	appConfig *config.Config,
	infra *sharedmodules.InfraModule,
	logger *slog.Logger,
) (runtimeAlarmScheduler, error) {
	if appConfig == nil {
		return nil, fmt.Errorf("config is required")
	}
	if infra == nil || infra.Postgres == nil {
		return nil, fmt.Errorf("postgres is required")
	}
	irisClient, err := providers.ProvideIrisKaringClient(
		logger,
		iris.WithBaseURL(appConfig.Iris.BaseURL),
		iris.WithBotToken(appConfig.Iris.BotToken),
	)
	if err != nil {
		return nil, fmt.Errorf("init alarm-worker notification egress iris client: %w", err)
	}
	irisSender := egress.NewIrisMessageSender(irisClient)

	alarmDispatchRunner, err := buildAlarmDispatchRunner(infra, irisSender, logger)
	if err != nil {
		return nil, err
	}
	youtubeOutboxDispatcher, err := buildYouTubeOutboxDispatcher(infra, buildYouTubeOutboxSender(irisSender, alarmDispatchMessageStrings(infra, logger)), logger)
	if err != nil {
		return nil, err
	}
	deliveryOutboxDispatcher, err := buildDeliveryOutboxDispatcher(infra, irisSender, logger)
	if err != nil {
		return nil, err
	}

	runners := []namedRuntimeScheduler{
		{name: "alarm-dispatch", scheduler: alarmDispatchRunner},
		{name: "alarm-dispatch-maintenance", scheduler: buildAlarmDispatchMaintenanceRunner(infra, logger)},
		{name: "youtube-outbox", scheduler: youtubeOutboxDispatcher},
		{name: "notification-delivery-outbox", scheduler: deliveryOutboxDispatcher},
	}
	return notificationEgressRunner{
		runners:      runners,
		leaseCache:   infra.Cache,
		leaseEnabled: parseBoolEnv("ALARM_WORKER_EGRESS_LEASE_ENABLED", true),
		logger:       logger,
	}, nil
}

func buildDeliveryOutboxDispatcher(
	infra *sharedmodules.InfraModule,
	sender delivery.MessageSender,
	logger *slog.Logger,
) (runtimeAlarmScheduler, error) {
	if !parseBoolEnv("DELIVERY_DISPATCHER_ENABLED", true) {
		if logger != nil {
			logger.Info("Notification delivery outbox dispatcher disabled")
		}
		return nil, nil
	}
	if infra == nil || infra.Postgres == nil {
		return nil, fmt.Errorf("postgres is required")
	}
	dispatcher := delivery.NewDispatcher(
		delivery.NewOutboxRepository(infra.Postgres, logger),
		sender,
		logger,
		delivery.DefaultDispatcherConfig(),
	)
	return deliveryOutboxDispatcherRunner{
		dispatcher: dispatcher,
		logger:     logger,
	}, nil
}

func buildAlarmDispatchRunner(
	infra *sharedmodules.InfraModule,
	sender alarmDispatchSender,
	logger *slog.Logger,
) (runtimeAlarmScheduler, error) {
	if !parseBoolEnv("ALARM_DISPATCH_CONSUMER_ENABLED", true) {
		if logger != nil {
			logger.Info("Alarm dispatch consumer disabled")
		}
		return nil, nil
	}
	if infra == nil {
		return nil, fmt.Errorf("infra is required")
	}
	if err := rejectRemovedAlarmDispatchModeEnv(); err != nil {
		return nil, err
	}
	if infra.Postgres == nil {
		return nil, fmt.Errorf("postgres is required")
	}

	maxBatch := parsePositiveIntEnv("ALARM_DISPATCH_MAX_BATCH", 50)
	lease := parsePositiveDurationSecondsEnv("ALARM_DISPATCH_LEASE_SECONDS", 60*time.Second)
	return &alarmDispatchRunner{
		consumer: dispatchoutbox.NewConsumer(
			dispatchoutbox.NewPgxRepository(infra.Postgres, logger),
			logger,
			dispatchoutbox.WithLease(lease),
			dispatchoutbox.WithRecoveryInterval(parsePositiveDurationMSEnv("ALARM_DISPATCH_RECOVERY_INTERVAL_MS", 30*time.Second)),
			dispatchoutbox.WithRecoveryBatchSize(parsePositiveIntEnv("ALARM_DISPATCH_RECOVERY_BATCH_SIZE", 100)),
			dispatchoutbox.WithClaimKeyReleaser(infra.Cache),
		),
		sender:             sender,
		renderer:           template.NewRenderer(infra.Postgres.GetPool(), logger),
		messageStrings:     alarmDispatchMessageStrings(infra, logger),
		idleWaiter:         newAlarmDispatchWakeupWaiter(infra.Cache, logger),
		karingEnabled:      parseAlarmDispatchKaringEnabled(),
		consumerMode:       "pg",
		postSendQuarantine: true,
		maxBatch:           maxBatch,
		maxBatchesPerWake:  parsePositiveIntEnv("ALARM_DISPATCH_MAX_BATCHES_PER_WAKE", 20),
		logger:             logger,
	}, nil
}

func parseAlarmDispatchKaringEnabled() bool {
	return parseBoolEnv("ALARM_DISPATCH_KARING_ENABLED", false)
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
	if !parseBoolEnv("YOUTUBE_OUTBOX_KARING_ENABLED", false) {
		return irisSender
	}
	return newYouTubeOutboxKaringSender(irisSender, messageStrings)
}

func buildYouTubeOutboxDispatcher(
	infra *sharedmodules.InfraModule,
	sender delivery.MessageSender,
	logger *slog.Logger,
) (runtimeAlarmScheduler, error) {
	if !parseBoolEnv("YOUTUBE_OUTBOX_DISPATCHER_ENABLED", false) {
		if logger != nil {
			logger.Info("YouTube outbox dispatcher disabled")
		}
		return nil, nil
	}
	if infra == nil || infra.Postgres == nil {
		return nil, fmt.Errorf("postgres is required")
	}

	dispatchConfig := youtubeoutbox.DefaultConfig()
	dispatcher := youtubeoutbox.NewDispatcher(
		infra.Postgres.GetPool(),
		infra.Cache,
		sender,
		template.NewRenderer(infra.Postgres.GetPool(), logger),
		logger,
		&dispatchConfig,
	)
	return youtubeOutboxDispatcherRunner{
		dispatcher: dispatcher,
		logger:     logger,
	}, nil
}
