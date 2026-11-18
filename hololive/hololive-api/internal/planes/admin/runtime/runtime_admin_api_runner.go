package adminruntime

import (
	"context"
	"sync"

	"github.com/park285/shared-go/v2/pkg/panicguard"

	applifecycle "github.com/kapu/hololive-shared/pkg/applifecycle"
	"github.com/kapu/hololive-shared/pkg/panicscope"
)

func (r *AdminAPIRuntime) Start(ctx context.Context, errCh chan<- error) {
	if r == nil {
		return
	}

	r.stateMu.Lock()

	if r.started || r.closing {
		r.stateMu.Unlock()

		return
	}

	r.started = true

	tasks := r.backgroundTasks()
	backgroundCtx := ctx

	if len(tasks) > 0 {
		backgroundCtx, r.backgroundCancel = context.WithCancel(ctx)
		r.backgroundDone = make(chan struct{})
	}

	backgroundDone := r.backgroundDone
	r.stateMu.Unlock()

	if backgroundDone != nil {
		go func() {
			defer close(backgroundDone)

			var wg sync.WaitGroup

			for name, task := range tasks {
				wg.Go(func() {
					panicguard.Run(r.Logger, panicscope.BackgroundTask, name, func() { task.Start(backgroundCtx) })
				})
			}

			wg.Wait()
		}()
	}

	applifecycle.Start(ctx, errCh, applifecycle.StartHooks{
		Logger:          r.Logger,
		ServerAddr:      r.ServerAddr,
		StartHTTPServer: r.StartHTTPServer,
	})
}

func (r *AdminAPIRuntime) StartHTTPServer(errCh chan<- error) {
	if r == nil {
		return
	}

	r.stateMu.Lock()
	defer r.stateMu.Unlock()

	if r.closing {
		return
	}

	if r.HTTPServers != nil {
		r.started = true
	}

	r.httpStartOnce.Do(func() { r.HTTPServers.Start(r.Logger, errCh) })
}

func (r *AdminAPIRuntime) backgroundTasks() map[string]backgroundTask {
	tasks := make(map[string]backgroundTask, 2)

	if r.PhotoSync != nil {
		tasks["admin-photo-sync"] = r.PhotoSync
	}

	if r.SchedulePoster != nil {
		tasks["admin-schedule-poster"] = r.SchedulePoster
	}

	return tasks
}
