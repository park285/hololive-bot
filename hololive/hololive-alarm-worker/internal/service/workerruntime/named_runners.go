package workerruntime

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/park285/shared-go/v2/pkg/panicguard"
	"golang.org/x/sync/errgroup"

	"github.com/kapu/hololive-shared/pkg/panicscope"
)

func runNamedSchedulers(ctx context.Context, logger *slog.Logger, prefix string, runners []NamedScheduler) error {
	group, groupCtx := errgroup.WithContext(ctx)

	for _, runner := range runners {
		if runner.Scheduler == nil {
			continue
		}

		group.Go(func() error {
			return panicguard.RunE(logger, panicscope.BackgroundTask, prefix+runner.Name, func() error {
				return runner.Scheduler.Start(groupCtx)
			})
		})
	}

	if err := group.Wait(); err != nil {
		return fmt.Errorf("wait named runners: %w", err)
	}

	return nil
}
