package workerapp

import (
	"log/slog"
	"time"

	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/service/member"

	"github.com/kapu/hololive-alarm-worker/internal/service/celebration"
	"github.com/kapu/hololive-alarm-worker/internal/service/envconfig"
)

func buildCelebrationRunnerScheduler(
	infra *sharedmodules.InfraModule,
	foundation *alarmFoundation,
	publishConfig queue.PublishConfig,
	logger *slog.Logger,
) runtimeAlarmScheduler {
	if !envconfig.ParseBool("CELEBRATION_RUNNER_ENABLED", false) {
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

	return celebration.NewRunner(memberRepo, alarmRepo, publisher, logger, celebration.RunnerConfig{
		CheckHourKST: envconfig.ParseNonNegativeInt("CELEBRATION_CHECK_HOUR_KST", 0),
		RunInterval:  envconfig.ParsePositiveDurationMS("CELEBRATION_RUN_INTERVAL_MS", time.Hour),
	})
}

func buildBirthdayStreamRunnerScheduler(
	infra *sharedmodules.InfraModule,
	foundation *alarmFoundation,
	publishConfig queue.PublishConfig,
	logger *slog.Logger,
) runtimeAlarmScheduler {
	if !envconfig.ParseBool("BIRTHDAY_STREAM_RUNNER_ENABLED", false) {
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

	return celebration.NewBirthdayStreamRunner(
		memberRepo,
		alarmRepo,
		celebration.NewPgxStore(infra.Postgres.GetPool()),
		publisher,
		logger,
		celebration.BirthdayStreamRunnerConfig{
			RunInterval:      envconfig.ParsePositiveDurationMS("BIRTHDAY_STREAM_POLL_INTERVAL_MS", 30*time.Minute),
			SessionFreshness: envconfig.ParsePositiveDurationMS("BIRTHDAY_STREAM_SESSION_FRESHNESS_MS", 30*time.Minute),
		},
	)
}
