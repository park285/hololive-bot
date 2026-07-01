package runtime

import "context"

// Start는 통합 hololive-api 프로세스가 쓰는 non-blocking 컴포넌트 lifecycle을 노출한다.
// 독립 실행 Run은 동일한 scheduler와 HTTP startup primitive를 그대로 사용한다.
func (r *LLMSchedulerRuntime) Start(ctx context.Context, errCh chan<- error) {
	if r == nil {
		return
	}
	r.startSchedulers(ctx)
	r.startHTTPServer(errCh)
}
