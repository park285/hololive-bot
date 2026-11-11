package workerapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	workerconfig "github.com/kapu/hololive-alarm-worker/internal/config"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/alarmstore"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/queue"
	xspacesworker "github.com/kapu/hololive-alarm-worker/internal/service/xspaces"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	sessions "github.com/kapu/hololive-shared/pkg/service/xspaces"
)

func buildXSpacesRunner(xSpaces workerconfig.XSpacesConfig, infra *sharedmodules.InfraModule, foundation *alarmFoundation, publishConfig queue.PublishConfig, logger *slog.Logger) optionalRuntimeSchedulerResult {
	config, err := xspacesworker.LoadConfig(xSpaces.ConfigFile)
	if errors.Is(err, xspacesworker.ErrDisabled) {
		return optionalRuntimeSchedulerResult{}
	}

	if err != nil {
		return optionalRuntimeSchedulerResult{err: fmt.Errorf("load X spaces config: %w", err)}
	}

	store, err := sessions.LoadStore(infra.Postgres.GetPool(), xSpaces.KeyFile)
	if err != nil {
		return optionalRuntimeSchedulerResult{err: fmt.Errorf("load X spaces sessions: %w", err)}
	}

	pub, err := queue.NewPublisher(infra.Cache, foundation.Outbox, publishConfig, logger)
	if err != nil {
		return optionalRuntimeSchedulerResult{err: fmt.Errorf("build X spaces publisher: %w", err)}
	}

	resolver := alarmstore.NewSubscriberResolver(infra.Cache, infra.Postgres.GetPool())

	runner, err := xspacesworker.NewRunner(*config, store, xspacesworker.StartStore{Pool: infra.Postgres.GetPool()},
		xspacesworker.ProcessCollector{}, pub,
		func(ctx context.Context, channelID string) ([]string, error) {
			return resolver.ResolveChannelSubscribersByType(ctx, channelID, domain.AlarmTypeLive)
		}, logger)
	if err != nil {
		return optionalRuntimeSchedulerResult{err: fmt.Errorf("build X spaces runner: %w", err)}
	}

	return optionalRuntimeSchedulerResult{scheduler: runner}
}
