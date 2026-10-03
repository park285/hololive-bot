package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/park285/shared-go/v2/pkg/panicguard"

	"github.com/kapu/hololive-shared/pkg/constants"
)

func (r *LLMSchedulerRuntime) beginSchedulerStop() <-chan struct{} {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	r.stopping = true
	if r.tasksCancel != nil {
		r.tasksCancel()
	}

	r.schedulersStopOnce.Do(func() {
		r.schedulersDone = make(chan struct{})

		go func() {
			defer close(r.schedulersDone)

			r.stopSchedulers()
		}()
	})

	return r.schedulersDone
}

func waitLLMRuntimeDone(ctx context.Context, done <-chan struct{}) error {
	select {
	case <-done:
		return nil
	default:
	}

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for LLM runtime: %w", ctx.Err())
	}
}

// Shutdown은 HTTP·scheduler의 단일 stop 작업을 caller의 종료 예산 안에서 기다린다.
// HTTP handler나 Stop이 취소를 무시해도 timeout은 반환하며 최초 종료 오류를 보존한다.
func (r *LLMSchedulerRuntime) Shutdown(ctx context.Context) error {
	if r == nil {
		return nil
	}

	done := r.beginSchedulerStop()
	httpErr := r.shutdownHTTPServer(ctx)

	if httpErr != nil && r.Logger != nil {
		r.Logger.Error("HTTP server shutdown error", slog.Any("error", httpErr))
	}

	stopErr := waitLLMRuntimeDone(ctx, done)

	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	if r.drainErr == nil {
		r.drainErr = errors.Join(httpErr, stopErr)
	}

	if r.httpStopErr != nil && !errors.Is(r.drainErr, r.httpStopErr) {
		r.drainErr = errors.Join(r.drainErr, r.httpStopErr)
	}

	return r.drainErr
}

func (r *LLMSchedulerRuntime) shutdownHTTPServer(ctx context.Context) error {
	if r.httpServers == nil {
		r.lifecycleMu.Lock()

		r.httpQuiesced = true
		r.lifecycleMu.Unlock()

		return nil
	}

	r.httpStopInit.Do(func() { r.httpStopGate = make(chan struct{}, 1) })

	// 이전 호출이 시한을 반환해도 실제 HTTP stop이 끝날 때까지 같은 owner가 gate를 보유한다.
	select {
	case r.httpStopGate <- struct{}{}:
	case <-ctx.Done():
		return fmt.Errorf("wait for LLM HTTP stop: %w", ctx.Err())
	}

	r.lifecycleMu.Lock()

	quiesced, previousErr := r.httpQuiesced, r.httpStopErr
	r.lifecycleMu.Unlock()

	if quiesced {
		<-r.httpStopGate

		return previousErr
	}

	done := make(chan error, 1)

	go func() {
		defer func() { <-r.httpStopGate }()

		stopErr := panicguard.RunE(r.Logger, panicguard.BackgroundTask, "llm-http-stop", func() error {
			if err := r.httpServers.Shutdown(ctx); err != nil {
				return fmt.Errorf("stop LLM HTTP servers: %w", err)
			}

			return nil
		})

		r.lifecycleMu.Lock()

		if stopErr != nil && r.httpStopErr == nil {
			r.httpStopErr = stopErr
		}

		// 성공한 Shutdown만 모든 H3·net/http handler가 join됐음을 증명한다.
		r.httpQuiesced = stopErr == nil

		reportErr := r.httpStopErr
		r.lifecycleMu.Unlock()

		done <- reportErr
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("join LLM HTTP stop: %w", ctx.Err())
	}
}

func (r *LLMSchedulerRuntime) quiesced() bool {
	r.lifecycleMu.Lock()

	httpQuiesced, schedulersDone := r.httpQuiesced, r.schedulersDone
	r.lifecycleMu.Unlock()

	if !httpQuiesced {
		return false
	}

	select {
	case <-schedulersDone:
		return true
	default:
		return false
	}
}

// CloseContext는 scheduler와 HTTP 요청이 끝난 뒤 한 resource owner의 정리를 시작한다.
// 과거 종료 오류와 실제 join 상태를 분리하여 후속 호출에서도 자원을 해제하고 오류를 보존한다.
func (r *LLMSchedulerRuntime) CloseContext(ctx context.Context) error {
	if r == nil {
		return nil
	}

	drainErr := r.Shutdown(ctx)
	if !r.quiesced() {
		return fmt.Errorf("stop LLM resource users: %w", drainErr)
	}

	if err := ctx.Err(); err != nil {
		return errors.Join(drainErr, fmt.Errorf("close LLM resources: %w", err))
	}

	r.resourcesCloseOnce.Do(func() {
		r.resourcesDone = make(chan struct{})

		go func() {
			defer close(r.resourcesDone)

			r.resourcesErr = panicguard.RunE(r.Logger, panicguard.BackgroundTask, "llm-resource-close", func() error {
				r.Managed.Close()

				return nil
			})
		}()
	})

	if err := waitLLMRuntimeDone(ctx, r.resourcesDone); err != nil {
		return errors.Join(drainErr, fmt.Errorf("join LLM resource cleanup: %w", err))
	}

	return errors.Join(drainErr, r.resourcesErr)
}

// Close는 기존 내부 caller에도 유한 종료 예산을 제공한다. 통합 runtime은 공유 예산의 CloseContext를 사용한다.
func (r *LLMSchedulerRuntime) Close() {
	if r == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), constants.AppTimeout.Shutdown)

	defer cancel()

	if err := r.CloseContext(ctx); err != nil && r.Logger != nil {
		r.Logger.Error("LLM runtime close failed", slog.Any("error", err))
	}
}
