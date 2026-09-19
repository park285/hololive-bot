package workerapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	xspacesworker "github.com/kapu/hololive-alarm-worker/internal/service/xspaces"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
	sessions "github.com/kapu/hololive-shared/pkg/service/xspaces"
)

func buildXSpacesRunner(infra *sharedmodules.InfraModule, foundation *alarmFoundation, publishConfig queue.PublishConfig, logger *slog.Logger) optionalRuntimeSchedulerResult {
	config, err := xspacesworker.LoadConfig()
	if errors.Is(err, xspacesworker.ErrDisabled) {
		return optionalRuntimeSchedulerResult{}
	}

	if err != nil {
		return optionalRuntimeSchedulerResult{err: fmt.Errorf("load X spaces config: %w", err)}
	}

	if infra == nil || infra.Postgres == nil {
		return optionalRuntimeSchedulerResult{err: errors.New("x spaces database unavailable")}
	}

	store, err := sessions.LoadStore(infra.Postgres.GetPool())
	if err != nil {
		return optionalRuntimeSchedulerResult{err: fmt.Errorf("load X spaces sessions: %w", err)}
	}

	if store == nil {
		return optionalRuntimeSchedulerResult{err: errors.New("x spaces config requires X_SPACES_KEY_FILE")}
	}

	pub := queue.NewPublisher(infra.Cache, logger, queue.WithOutbox(foundation.Outbox), queue.WithWakeupEnabled(publishConfig.WakeupEnabled), queue.WithMaxDeliveriesPerBatch(publishConfig.MaxDeliveriesPerBatch))

	runner, err := xspacesworker.NewRunner(*config, store, xspacesworker.StartStore{Pool: infra.Postgres.GetPool()},
		xspacesworker.ProcessCollector{}, pub,
		func(ctx context.Context, channelID string) ([]string, error) {
			return sharedalarm.ResolveChannelSubscribersByType(ctx, infra.Cache, infra.Postgres.GetPool(), channelID, domain.AlarmTypeLive)
		}, logger)
	if err != nil {
		return optionalRuntimeSchedulerResult{err: fmt.Errorf("build X spaces runner: %w", err)}
	}

	return optionalRuntimeSchedulerResult{scheduler: runner}
}
