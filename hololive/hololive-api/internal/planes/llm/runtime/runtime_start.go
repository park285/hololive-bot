package runtime

import "context"

// Start는 통합 hololive-api 프로세스가 쓰는 non-blocking 컴포넌트 lifecycle을 노출한다.
// Shutdown 뒤 재시작하지 않으며 background의 취소권을 runtime이 소유한다.
func (r *LLMSchedulerRuntime) Start(ctx context.Context, errCh chan<- error) {
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

	r.startSchedulers(runCtx)
	r.startHTTPServer(errCh)
}
