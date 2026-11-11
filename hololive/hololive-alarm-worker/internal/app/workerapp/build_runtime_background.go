package workerapp

import (
	"errors"
	"fmt"
	"log/slog"

	workerconfig "github.com/kapu/hololive-alarm-worker/internal/config"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/alarmstore"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/queue"
	"github.com/kapu/hololive-alarm-worker/internal/service/celebration"
	"github.com/kapu/hololive-alarm-worker/internal/service/workerruntime"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/member"
)

type alarmWorkerBackgroundRunners struct {
	celebration    workerruntime.Scheduler
	birthdayStream workerruntime.Scheduler
	xSpaces        workerruntime.Scheduler
}

// buildAlarmWorkerBackgroundRunners는 설정에서 켜진 X Spaces·celebration·birthday stream runner를 만든다.
// 세 runner 모두 PostgreSQL을 쓰므로, PostgreSQL이 없는 infra는 runner를 끄고 넘어가지 않고 조립 오류로 돌려준다.
func buildAlarmWorkerBackgroundRunners(
	appConfig *workerconfig.RuntimeConfig,
	infra *sharedmodules.InfraModule,
	foundation *alarmFoundation,
	publishConfig queue.PublishConfig,
	logger *slog.Logger,
) (runners alarmWorkerBackgroundRunners, stage string, err error) {
	if infra == nil || infra.Postgres == nil {
		return alarmWorkerBackgroundRunners{}, "background runners", errors.New("postgres is required")
	}

	xSpaces := buildXSpacesRunner(appConfig.XSpaces, infra, foundation, publishConfig, logger)
	if xSpaces.err != nil {
		return alarmWorkerBackgroundRunners{}, "X spaces", xSpaces.err
	}

	celebrationRunner := buildCelebrationRunnerScheduler(appConfig.Celebration, infra, foundation, publishConfig, logger)
	if celebrationRunner.err != nil {
		return alarmWorkerBackgroundRunners{}, "celebration runner", celebrationRunner.err
	}

	birthdayStreamRunner := buildBirthdayStreamRunnerScheduler(appConfig.BirthdayStream, infra, foundation, publishConfig, logger)
	if birthdayStreamRunner.err != nil {
		return alarmWorkerBackgroundRunners{}, "birthday stream runner", birthdayStreamRunner.err
	}

	return alarmWorkerBackgroundRunners{
		celebration:    celebrationRunner.scheduler,
		birthdayStream: birthdayStreamRunner.scheduler,
		xSpaces:        xSpaces.scheduler,
	}, "", nil
}

func buildCelebrationRunnerScheduler(
	config workerconfig.CelebrationRunnerConfig,
	infra *sharedmodules.InfraModule,
	foundation *alarmFoundation,
	publishConfig queue.PublishConfig,
	logger *slog.Logger,
) optionalRuntimeSchedulerResult {
	if !config.Enabled {
		logWorkerDisabled(logger, "Celebration runner disabled")

		return optionalRuntimeSchedulerResult{}
	}

	memberRepo := member.NewMemberRepository(infra.Postgres)
	alarmRepo := alarmstore.NewRepository(infra.Postgres, logger)

	publisher, err := queue.NewPublisher(infra.Cache, foundation.Outbox, publishConfig, logger)
	if err != nil {
		return optionalRuntimeSchedulerResult{err: fmt.Errorf("build celebration publisher: %w", err)}
	}

	return optionalRuntimeSchedulerResult{scheduler: celebration.NewRunner(memberRepo, alarmRepo, publisher, logger, celebration.RunnerConfig{
		CheckHourKST: config.CheckHourKST,
		RunInterval:  config.RunInterval,
	})}
}

func buildBirthdayStreamRunnerScheduler(
	config workerconfig.BirthdayStreamRunnerConfig,
	infra *sharedmodules.InfraModule,
	foundation *alarmFoundation,
	publishConfig queue.PublishConfig,
	logger *slog.Logger,
) optionalRuntimeSchedulerResult {
	if !config.Enabled {
		logWorkerDisabled(logger, "Birthday stream runner disabled")

		return optionalRuntimeSchedulerResult{}
	}

	memberRepo := member.NewMemberRepository(infra.Postgres)

	publisher, err := queue.NewPublisher(infra.Cache, foundation.Outbox, publishConfig, logger)
	if err != nil {
		return optionalRuntimeSchedulerResult{err: fmt.Errorf("build birthday stream publisher: %w", err)}
	}

	return optionalRuntimeSchedulerResult{scheduler: celebration.NewBirthdayStreamRunner(
		memberRepo,
		celebration.NewPgxStore(infra.Postgres.GetPool()),
		publisher,
		logger,
		celebration.BirthdayStreamRunnerConfig{RunInterval: config.RunInterval, SessionFreshness: config.SessionFreshness},
	)}
}
