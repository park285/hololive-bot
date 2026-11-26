package botruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/park285/shared-go/v2/pkg/panicguard"

	"github.com/kapu/hololive-shared/pkg/panicscope"
)

func (r *BotRuntime) Start(ctx context.Context, errCh chan<- error) {
	if r == nil {
		return
	}

	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	if r.started || r.stopping {
		return
	}

	r.started = true

	runCtx, cancel := context.WithCancel(ctx)

	r.tasksCancel = cancel
	r.tasksDone = make(chan struct{})

	if r.durable != nil {
		r.durable.Start(runCtx)
	}

	r.startWorkerProfileChecker(runCtx)

	r.startBot(runCtx, errCh)

	if r.h3CertReloadStart != nil {
		r.tasksWG.Go(func() {
			if err := panicguard.RunE(r.Logger, panicscope.BackgroundTask, "bot-h3-certificate-reload", func() error {
				r.h3CertReloadStart(runCtx)

				return nil
			}); err != nil {
				r.recordBackgroundError(runCtx, errCh, fmt.Errorf("bot H3 certificate reload: %w", err))
			}
		})
	}

	if r.broadcastTypes != nil {
		r.tasksWG.Go(func() {
			panicguard.Run(r.Logger, panicscope.BackgroundTask, "bot-broadcast-type-decision", func() {
				r.broadcastTypes.Start(runCtx)
			})
		})
	}

	go panicguard.Run(r.Logger, panicscope.BackgroundTask, "bot-runtime-task-join", func() {
		r.tasksWG.Wait()
		close(r.tasksDone)
	})

	r.StartHTTPServer(errCh)

	if r.Logger != nil && r.ServerAddr != "" {
		r.Logger.Info("HTTP server started", slog.String("addr", r.ServerAddr))
	}
}

func (r *BotRuntime) startBot(runCtx context.Context, errCh chan<- error) {
	if r.Bot == nil {
		return
	}

	r.tasksWG.Go(func() {
		if err := panicguard.RunE(r.Logger, panicscope.BackgroundTask, "bot-runtime", func() error {
			return r.Bot.Start(runCtx)
		}); err != nil && !isContextTermination(err, runCtx.Err()) {
			r.recordBackgroundError(runCtx, errCh, fmt.Errorf("bot runtime error: %w", err))
		}
	})

	r.tasksWG.Go(func() {
		panicguard.Run(r.Logger, panicscope.BackgroundTask, "bot-schedule-poster-notices", func() {
			r.Bot.RunScheduleNotices(runCtx)
		})
	})
}

func (r *BotRuntime) recordBackgroundError(ctx context.Context, errCh chan<- error, err error) {
	r.lifecycleMu.Lock()

	r.backgroundErr = errors.Join(r.backgroundErr, err)
	r.lifecycleMu.Unlock()

	if errCh != nil {
		select {
		case errCh <- err:
		case <-ctx.Done():
		}
	}

	r.logError("bot background task failed", err)
}

func isContextTermination(err, contextErr error) bool {
	if contextErr == nil {
		return false
	}

	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		foundLeaf := false

		for _, child := range joined.Unwrap() {
			if child == nil {
				continue
			}

			foundLeaf = true

			if !isContextTermination(child, contextErr) {
				return false
			}
		}

		if foundLeaf {
			return true
		}
	}

	if wrapped := errors.Unwrap(err); wrapped != nil {
		return isContextTermination(wrapped, contextErr)
	}

	return errors.Is(err, contextErr)
}
