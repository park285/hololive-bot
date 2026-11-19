package botruntime

import (
	"context"
	"errors"
	"fmt"

	"github.com/park285/shared-go/v2/pkg/panicguard"

	"github.com/kapu/hololive-shared/pkg/panicscope"
)

func (r *BotRuntime) cancelBackgroundTasks() {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	r.stopping = true

	if r.tasksCancel != nil {
		r.tasksCancel()
	}
}

func (r *BotRuntime) backgroundTasksJoined() bool {
	r.lifecycleMu.Lock()

	done := r.tasksDone
	r.lifecycleMu.Unlock()

	if done == nil {
		return true
	}

	select {
	case <-done:
		return true
	default:
		return false
	}
}

func (r *BotRuntime) joinBackgroundTasks(ctx context.Context) error {
	r.lifecycleMu.Lock()

	done := r.tasksDone
	r.lifecycleMu.Unlock()

	if done != nil {
		if err := waitForDurableStop(ctx, done); err != nil {
			return fmt.Errorf("join bot background tasks: %w", err)
		}
	}

	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	return r.backgroundErr
}

func (r *BotRuntime) CloseContext(ctx context.Context) error {
	if r == nil {
		return nil
	}

	var shutdownErr error

	if r.quiesced.Load() {
		r.lifecycleMu.Lock()

		shutdownErr = r.shutdownErr
		r.lifecycleMu.Unlock()
	} else {
		shutdownErr = r.Shutdown(ctx)
	}

	if !r.quiesced.Load() {
		return errors.Join(shutdownErr, errors.New("close bot runtime: resource users have not stopped"))
	}

	if err := ctx.Err(); err != nil {
		return errors.Join(shutdownErr, fmt.Errorf("close bot resources: %w", err))
	}

	r.resourcesCloseOnce.Do(func() {
		r.resourcesDone = make(chan struct{})

		go func() {
			defer close(r.resourcesDone)

			r.resourcesErr = panicguard.RunE(r.Logger, panicscope.BackgroundTask, "bot-resource-close", func() error {
				if r.cleanup == nil {
					return nil
				}

				return r.cleanup()
			})
		}()
	})

	if err := waitForDurableStop(ctx, r.resourcesDone); err != nil {
		return errors.Join(shutdownErr, fmt.Errorf("join bot resource cleanup: %w", err))
	}

	return errors.Join(shutdownErr, r.resourcesErr)
}

func (r *BotRuntime) ResourceUsersJoined() bool {
	return r == nil || r.quiesced.Load()
}
