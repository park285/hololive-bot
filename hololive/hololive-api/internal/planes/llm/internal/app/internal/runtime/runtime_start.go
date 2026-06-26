package runtime

import "context"

// Start exposes the non-blocking component lifecycle used by the unified
// hololive-api process. Standalone Run continues to use the same scheduler and
// HTTP startup primitives.
func (r *LLMSchedulerRuntime) Start(ctx context.Context, errCh chan<- error) {
	if r == nil {
		return
	}
	r.startSchedulers(ctx)
	r.startHTTPServer(errCh)
}
