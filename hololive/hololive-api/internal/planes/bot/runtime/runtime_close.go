package botruntime

import (
	"context"
	"errors"
	"fmt"

	"github.com/park285/shared-go/v2/pkg/panicguard"
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

// CloseContext는 공유 종료 예산으로 같은 작업을 join한 뒤 같은 owner의 정리를 기다린다.
// 작업 join 미완료에서는 DB/cache를 닫지 않는다. 정리 중 deadline이 끝나도 두 번째 정리를 시작하지 않는다.
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

			r.resourcesErr = panicguard.RunE(r.Logger, panicguard.BackgroundTask, "bot-resource-close", func() error {
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
