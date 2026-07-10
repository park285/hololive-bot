package workerapp

import (
	"log/slog"
	"time"

	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/service/member"
)

func buildCelebrationRunnerScheduler(
	infra *sharedmodules.InfraModule,
	foundation *alarmFoundation,
	publishConfig queue.PublishConfig,
	logger *slog.Logger,
) runtimeAlarmScheduler {
	if !parseBoolEnv("CELEBRATION_RUNNER_ENABLED", false) {
		if logger != nil {
			logger.Info("Celebration runner disabled")
		}
		return nil
	}
	if infra == nil || infra.Postgres == nil {
		return nil
	}

	memberRepo := member.NewMemberRepository(infra.Postgres, logger)
	alarmRepo := sharedalarm.NewRepository(infra.Postgres, logger)
	publisher := queue.NewPublisher(
		infra.Cache,
		logger,
		queue.WithOutbox(foundation.Outbox),
		queue.WithWakeupEnabled(publishConfig.WakeupEnabled),
		queue.WithMaxDeliveriesPerBatch(publishConfig.MaxDeliveriesPerBatch),
	)

	return &celebrationRunner{
		memberRepo:   memberRepo,
		alarmRepo:    alarmRepo,
		publisher:    publisher,
		logger:       logger,
		checkHourKST: parseNonNegativeIntEnv("CELEBRATION_CHECK_HOUR_KST", 0),
		runInterval:  parsePositiveDurationMSEnv("CELEBRATION_RUN_INTERVAL_MS", time.Hour),
	}
}

func buildBirthdayStreamRunnerScheduler(
	infra *sharedmodules.InfraModule,
	foundation *alarmFoundation,
	publishConfig queue.PublishConfig,
	logger *slog.Logger,
) runtimeAlarmScheduler {
	if !parseBoolEnv("BIRTHDAY_STREAM_RUNNER_ENABLED", false) {
		if logger != nil {
			logger.Info("Birthday stream runner disabled")
		}
		return nil
	}
	if infra == nil || infra.Postgres == nil {
		return nil
	}

	memberRepo := member.NewMemberRepository(infra.Postgres, logger)
	alarmRepo := sharedalarm.NewRepository(infra.Postgres, logger)
	publisher := queue.NewPublisher(
		infra.Cache,
		logger,
		queue.WithOutbox(foundation.Outbox),
		queue.WithWakeupEnabled(publishConfig.WakeupEnabled),
		queue.WithMaxDeliveriesPerBatch(publishConfig.MaxDeliveriesPerBatch),
	)

	return &birthdayStreamRunner{
		memberRepo:       memberRepo,
		alarmRepo:        alarmRepo,
		sessions:         &birthdayStreamPgxStore{db: infra.Postgres.GetPool()},
		publisher:        publisher,
		logger:           logger,
		runInterval:      parsePositiveDurationMSEnv("BIRTHDAY_STREAM_POLL_INTERVAL_MS", 30*time.Minute),
		sessionFreshness: parsePositiveDurationMSEnv("BIRTHDAY_STREAM_SESSION_FRESHNESS_MS", 30*time.Minute),
	}
}
