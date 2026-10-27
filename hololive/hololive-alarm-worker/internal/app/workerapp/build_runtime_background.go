package workerapp

import (
	"fmt"
	"log/slog"
	"time"

	envutil "github.com/park285/shared-go/v2/pkg/envutil"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/alarmstore"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/queue"
	"github.com/kapu/hololive-alarm-worker/internal/service/celebration"
	"github.com/kapu/hololive-shared/pkg/config/envload"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/member"
)

func buildCelebrationRunnerScheduler(
	infra *sharedmodules.InfraModule,
	foundation *alarmFoundation,
	publishConfig queue.PublishConfig,
	logger *slog.Logger,
) optionalRuntimeSchedulerResult {
	// 잘못된 스위치 값을 "꺼짐"으로 읽고 넘어가지 않고 기동 오류로 드러낸다.
	enabled, err := envutil.BoolE("CELEBRATION_RUNNER_ENABLED", false)
	if err != nil {
		return optionalRuntimeSchedulerResult{err: fmt.Errorf("celebration runner enabled: %w", err)}
	}

	if !enabled {
		if logger != nil {
			logger.Info("Celebration runner disabled")
		}

		return optionalRuntimeSchedulerResult{}
	}

	config, err := loadCelebrationRunnerConfig()
	if err != nil {
		return optionalRuntimeSchedulerResult{err: err}
	}

	if infra == nil || infra.Postgres == nil {
		return optionalRuntimeSchedulerResult{}
	}

	memberRepo := member.NewMemberRepository(infra.Postgres, logger)
	alarmRepo := alarmstore.NewRepository(infra.Postgres, logger)
	publisher, err := queue.NewPublisher(infra.Cache, foundation.Outbox, publishConfig, logger)
	if err != nil {
		return optionalRuntimeSchedulerResult{err: fmt.Errorf("build celebration publisher: %w", err)}
	}

	return optionalRuntimeSchedulerResult{scheduler: celebration.NewRunner(memberRepo, alarmRepo, publisher, logger, config)}
}

// runner 설정은 infra 확인보다 먼저 읽어, 켜진 runner의 잘못된 값이 기본값으로 바뀌지 않고 기동 오류가 되게 한다.
func loadCelebrationRunnerConfig() (celebration.RunnerConfig, error) {
	checkHour, err := parseHourOfDayEnv("CELEBRATION_CHECK_HOUR_KST", 0)
	if err != nil {
		return celebration.RunnerConfig{}, fmt.Errorf("celebration runner config: %w", err)
	}

	runInterval, err := envload.RequiredMillisDurationEnv("CELEBRATION_RUN_INTERVAL_MS", time.Hour)
	if err != nil {
		return celebration.RunnerConfig{}, fmt.Errorf("celebration runner config: %w", err)
	}

	return celebration.RunnerConfig{CheckHourKST: checkHour, RunInterval: runInterval}, nil
}

func buildBirthdayStreamRunnerScheduler(
	infra *sharedmodules.InfraModule,
	foundation *alarmFoundation,
	publishConfig queue.PublishConfig,
	logger *slog.Logger,
) optionalRuntimeSchedulerResult {
	enabled, err := envutil.BoolE("BIRTHDAY_STREAM_RUNNER_ENABLED", false)
	if err != nil {
		return optionalRuntimeSchedulerResult{err: fmt.Errorf("birthday stream runner enabled: %w", err)}
	}

	if !enabled {
		if logger != nil {
			logger.Info("Birthday stream runner disabled")
		}

		return optionalRuntimeSchedulerResult{}
	}

	config, err := loadBirthdayStreamRunnerConfig()
	if err != nil {
		return optionalRuntimeSchedulerResult{err: err}
	}

	if infra == nil || infra.Postgres == nil {
		return optionalRuntimeSchedulerResult{}
	}

	memberRepo := member.NewMemberRepository(infra.Postgres, logger)
	publisher, err := queue.NewPublisher(infra.Cache, foundation.Outbox, publishConfig, logger)
	if err != nil {
		return optionalRuntimeSchedulerResult{err: fmt.Errorf("build birthday stream publisher: %w", err)}
	}

	return optionalRuntimeSchedulerResult{scheduler: celebration.NewBirthdayStreamRunner(
		memberRepo,
		celebration.NewPgxStore(infra.Postgres.GetPool()),
		publisher,
		logger,
		config,
	)}
}

func loadBirthdayStreamRunnerConfig() (celebration.BirthdayStreamRunnerConfig, error) {
	runInterval, err := envload.RequiredMillisDurationEnv("BIRTHDAY_STREAM_POLL_INTERVAL_MS", 30*time.Minute)
	if err != nil {
		return celebration.BirthdayStreamRunnerConfig{}, fmt.Errorf("birthday stream runner config: %w", err)
	}

	sessionFreshness, err := envload.RequiredMillisDurationEnv("BIRTHDAY_STREAM_SESSION_FRESHNESS_MS", 30*time.Minute)
	if err != nil {
		return celebration.BirthdayStreamRunnerConfig{}, fmt.Errorf("birthday stream runner config: %w", err)
	}

	return celebration.BirthdayStreamRunnerConfig{RunInterval: runInterval, SessionFreshness: sessionFreshness}, nil
}
