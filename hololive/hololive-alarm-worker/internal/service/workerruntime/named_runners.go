package workerruntime

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/park285/shared-go/v2/pkg/panicguard"
	"golang.org/x/sync/errgroup"
)

// 첫 오류로 형제를 취소하되 시작한 모든 작업의 종료 뒤에만 반환합니다.
func runNamedSchedulers(ctx context.Context, logger *slog.Logger, prefix string, runners []NamedScheduler) error {
	group, groupCtx := errgroup.WithContext(ctx)

	for _, runner := range runners {
		if runner.Scheduler == nil {
			continue
		}

		group.Go(func() error {
			return panicguard.RunE(logger, panicguard.BackgroundTask, prefix+runner.Name, func() error {
				return runner.Scheduler.Start(groupCtx)
			})
		})
	}

	if err := group.Wait(); err != nil {
		return fmt.Errorf("wait named runners: %w", err)
	}

	return nil
}
