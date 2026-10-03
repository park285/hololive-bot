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
	"errors"
	"fmt"

	applifecycle "github.com/kapu/hololive-shared/pkg/applifecycle"
)

func (r *AdminAPIRuntime) Shutdown(ctx context.Context) error {
	if r == nil {
		return nil
	}

	r.stateMu.Lock()

	r.closing = true

	photoCancel, photoDone := r.photoCancel, r.photoDone
	r.stateMu.Unlock()

	if photoCancel != nil {
		photoCancel()
	}

	httpErr := applifecycle.Shutdown(ctx, applifecycle.ShutdownHooks{
		Logger:             r.Logger,
		ShutdownHTTPServer: r.ShutdownHTTPServer,
	})
	photoErr := waitAdminPhotoSync(ctx, photoDone)

	r.stateMu.Lock()

	if r.drainErr == nil {
		r.drainErr = errors.Join(httpErr, photoErr)
	}

	if r.httpStopErr != nil && !errors.Is(r.drainErr, r.httpStopErr) {
		r.drainErr = errors.Join(r.drainErr, r.httpStopErr)
	}

	resultErr := r.drainErr
	r.stateMu.Unlock()

	if resultErr != nil {
		return fmt.Errorf("shutdown: %w", resultErr)
	}

	return nil
}

func (r *AdminAPIRuntime) ShutdownHTTPServer(ctx context.Context) error {
	if r == nil {
		return nil
	}

	if r.HTTPServers == nil {
		r.stateMu.Lock()

		r.httpQuiesced = true
		r.stateMu.Unlock()

		return nil
	}

	r.httpStopInit.Do(func() { r.httpStopGate = make(chan struct{}, 1) })

	// 같은 listener의 Stop 작업은 한 owner만 실행한다. 미완료 작업은 다음 호출도 이 gate에서 기다린다.
	select {
	case r.httpStopGate <- struct{}{}:
	case <-ctx.Done():
		return fmt.Errorf("shutdown: wait for admin HTTP stop: %w", ctx.Err())
	}

	r.stateMu.Lock()

	quiesced, previousErr := r.httpQuiesced, r.httpStopErr
	r.stateMu.Unlock()

	if quiesced {
		<-r.httpStopGate

		return previousErr
	}

	done := make(chan error, 1)

	go func() {
		stopErr := r.HTTPServers.Shutdown(ctx)

		r.stateMu.Lock()

		if stopErr != nil && r.httpStopErr == nil {
			r.httpStopErr = stopErr
		}

		// 성공한 HTTP Shutdown은 net/http·quic-go가 실제 handler를 모두 join한 증거다.
		r.httpQuiesced = stopErr == nil

		reportErr := r.httpStopErr
		r.stateMu.Unlock()

		done <- reportErr

		<-r.httpStopGate
	}()

	select {
	case stopErr := <-done:
		if stopErr != nil {
			return fmt.Errorf("shutdown: %w", stopErr)
		}

		return nil
	case <-ctx.Done():
		return fmt.Errorf("shutdown: admin HTTP stop: %w", ctx.Err())
	}
}

func waitAdminPhotoSync(ctx context.Context, done <-chan struct{}) error {
	if done == nil {
		return nil
	}

	select {
	case <-done:
		return nil
	default:
	}

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("join admin photo sync: %w", ctx.Err())
	}
}
