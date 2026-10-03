// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package botruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/park285/shared-go/v2/pkg/panicguard"
)

// Start는 background 작업을 등록한 뒤 ingress를 연다. Shutdown 이후 다시 시작하지 않는다.
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

	if r.Bot != nil {
		r.tasksWG.Go(func() {
			// readiness 자체 만료는 기동 실패다. runtime 취소로 끝난 작업만 정상 종료로 본다.
			if err := panicguard.RunE(r.Logger, panicguard.BackgroundTask, "bot-runtime", func() error {
				return r.Bot.Start(runCtx)
			}); err != nil && !isContextTermination(err, runCtx.Err()) {
				r.recordBackgroundError(runCtx, errCh, fmt.Errorf("bot runtime error: %w", err))
			}
		})
	}

	if r.h3CertReloadStart != nil {
		r.tasksWG.Go(func() {
			if err := panicguard.RunE(r.Logger, panicguard.BackgroundTask, "bot-h3-certificate-reload", func() error {
				r.h3CertReloadStart(runCtx)

				return nil
			}); err != nil {
				r.recordBackgroundError(runCtx, errCh, fmt.Errorf("bot H3 certificate reload: %w", err))
			}
		})
	}

	go panicguard.Run(r.Logger, panicguard.BackgroundTask, "bot-runtime-task-join", func() {
		r.tasksWG.Wait()
		close(r.tasksDone)
	})

	r.StartHTTPServer(errCh)

	if r.Logger != nil && r.ServerAddr != "" {
		r.Logger.Info("HTTP server started", slog.String("addr", r.ServerAddr))
	}
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

// 모든 말단 원인이 runtime 종료 원인과 같아야 정상 종료다.
// 종료 직전에 먼저 발생한 자체 timeout과 중첩된 실제 오류는 보존한다.
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
