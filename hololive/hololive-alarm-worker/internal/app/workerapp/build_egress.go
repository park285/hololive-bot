package workerapp

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
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

	runners := []namedRuntimeScheduler{
		{name: "alarm-dispatch", scheduler: buildAlarmDispatchRunner(infra, irisSender, logger)},
		{name: "alarm-dispatch-maintenance", scheduler: buildAlarmDispatchMaintenanceRunner(infra, logger)},
		{name: "youtube-outbox", scheduler: buildYouTubeOutboxDispatcher(infra, buildYouTubeOutboxSender(irisSender), logger)},
		{name: "notification-delivery-outbox", scheduler: buildDeliveryOutboxDispatcher(infra, irisSender, logger)},
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
) runtimeAlarmScheduler {
	if !parseBoolEnv("DELIVERY_DISPATCHER_ENABLED", true) {
		if logger != nil {
			logger.Info("Notification delivery outbox dispatcher disabled")
		}
		return nil
	}
	dispatcher := delivery.NewDispatcher(
		delivery.NewOutboxRepository(infra.Postgres.GetGormDB(), logger),
		sender,
		logger,
		delivery.DefaultDispatcherConfig(),
	)
	return deliveryOutboxDispatcherRunner{
		dispatcher: dispatcher,
		logger:     logger,
	}
}

func buildAlarmDispatchRunner(
	infra *sharedmodules.InfraModule,
	sender alarmDispatchSender,
	logger *slog.Logger,
) runtimeAlarmScheduler {
	if !parseBoolEnv("ALARM_DISPATCH_CONSUMER_ENABLED", true) {
		if logger != nil {
			logger.Info("Alarm dispatch consumer disabled")
		}
		return nil
	}

	consumerMode := strings.ToLower(strings.TrimSpace(os.Getenv("ALARM_DISPATCH_CONSUMER_MODE")))
	maxBatch := parsePositiveIntEnv("ALARM_DISPATCH_MAX_BATCH", 50)
	karingEnabled := parseAlarmDispatchKaringEnabled()
	if consumerMode == "pg" {
		lease := parsePositiveDurationSecondsEnv("ALARM_DISPATCH_LEASE_SECONDS", 60*time.Second)
		return &alarmDispatchRunner{
			consumer: dispatchoutbox.NewConsumer(
				dispatchoutbox.NewPgxRepository(infra.Postgres, logger),
				logger,
				dispatchoutbox.WithLease(lease),
				dispatchoutbox.WithRecoveryInterval(parsePositiveDurationMSEnv("ALARM_DISPATCH_RECOVERY_INTERVAL_MS", 30*time.Second)),
				dispatchoutbox.WithRecoveryBatchSize(parsePositiveIntEnv("ALARM_DISPATCH_RECOVERY_BATCH_SIZE", 100)),
			),
			sender:             sender,
			idleWaiter:         newAlarmDispatchWakeupWaiter(infra.Cache, logger),
			karingEnabled:      karingEnabled,
			consumerMode:       "pg",
			postSendQuarantine: true,
			maxBatch:           maxBatch,
			maxBatchesPerWake:  parsePositiveIntEnv("ALARM_DISPATCH_MAX_BATCHES_PER_WAKE", 20),
			logger:             logger,
		}
	}
	return &alarmDispatchRunner{
		consumer:           queue.NewConsumer(infra.Cache, logger, queue.WithMaxBatch(maxBatch)),
		sender:             sender,
		karingEnabled:      karingEnabled,
		consumerMode:       "valkey",
		postSendQuarantine: true,
		maxBatch:           maxBatch,
		logger:             logger,
	}
}

func parseAlarmDispatchKaringEnabled() bool {
	return parseBoolEnv("ALARM_DISPATCH_KARING_ENABLED", false)
}

func buildYouTubeOutboxSender(irisSender *egress.IrisMessageSender) delivery.MessageSender {
	if !parseBoolEnv("YOUTUBE_OUTBOX_KARING_ENABLED", false) {
		return irisSender
	}
	return newYouTubeOutboxKaringSender(irisSender)
}

func buildYouTubeOutboxDispatcher(
	infra *sharedmodules.InfraModule,
	sender delivery.MessageSender,
	logger *slog.Logger,
) runtimeAlarmScheduler {
	if !parseBoolEnv("YOUTUBE_OUTBOX_DISPATCHER_ENABLED", false) {
		if logger != nil {
			logger.Info("YouTube outbox dispatcher disabled")
		}
		return nil
	}

	dispatcher := youtubeoutbox.NewDispatcher(
		infra.Postgres.GetGormDB(),
		infra.Cache,
		sender,
		template.NewRenderer(infra.Postgres.GetGormDB(), logger),
		logger,
		youtubeoutbox.DefaultConfig(),
	)
	return youtubeOutboxDispatcherRunner{
		dispatcher: dispatcher,
		logger:     logger,
	}
}
