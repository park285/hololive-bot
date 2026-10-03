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

package adminruntime

import (
	"context"
	"fmt"

	"github.com/park285/shared-go/v2/pkg/panicguard"

	applifecycle "github.com/kapu/hololive-shared/pkg/applifecycle"
)

func (r *AdminAPIRuntime) Run() error {
	if r == nil {
		return nil
	}

	if err := applifecycle.Run(r.Logger, r.Start, r.Shutdown); err != nil {
		return fmt.Errorf("run: %w", err)
	}

	return nil
}

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

	photoCtx := ctx

	if r.PhotoSync != nil {
		photoCtx, r.photoCancel = context.WithCancel(ctx)
		r.photoDone = make(chan struct{})
	}

	photoDone := r.photoDone
	r.stateMu.Unlock()

	if photoDone != nil {
		go panicguard.Run(r.Logger, panicguard.BackgroundTask, "admin-photo-sync", func() {
			defer close(photoDone)

			r.PhotoSync.Start(photoCtx)
		})
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
