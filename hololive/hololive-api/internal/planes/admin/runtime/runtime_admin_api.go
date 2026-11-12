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
	"log/slog"
	"sync"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

type backgroundTask interface {
	Start(ctx context.Context)
}

type AdminAPIRuntime struct {
	Config *apiconfig.AdminPlaneConfig
	Logger *slog.Logger

	ServerAddr     string
	HTTPServers    *sharedserver.RuntimeHTTPServers
	PhotoSync      backgroundTask
	SchedulePoster backgroundTask

	stateMu          sync.Mutex
	started          bool
	closing          bool
	backgroundCancel context.CancelFunc
	backgroundDone   chan struct{}

	httpStopInit  sync.Once
	httpStopGate  chan struct{}
	httpStartOnce sync.Once
	httpStopErr   error
	httpQuiesced  bool
	drainErr      error
	cleanup       func() error
	cleanupOnce   sync.Once
	cleanupDone   chan struct{}
	cleanupErr    error
}

// CloseContext는 live 작업을 cancel/join한 뒤 같은 자원 owner를 한 번 실행한다. Join 실패에서 DB·cache를 닫지 않는다.
func (r *AdminAPIRuntime) CloseContext(ctx context.Context) error {
	if r == nil {
		return nil
	}

	r.stateMu.Lock()

	r.closing = true

	started := r.started
	r.stateMu.Unlock()

	var drainErr error

	if started {
		drainErr = r.Shutdown(ctx)
		if !r.quiesced() {
			return fmt.Errorf("close admin runtime: drain: %w", drainErr)
		}
	}

	r.cleanupOnce.Do(func() {
		r.cleanupDone = make(chan struct{})

		go func() {
			defer close(r.cleanupDone)

			if r.cleanup != nil {
				r.cleanupErr = r.cleanup()
			}
		}()
	})

	select {
	case <-r.cleanupDone:
		return errors.Join(drainErr, r.cleanupErr)
	default:
	}

	select {
	case <-r.cleanupDone:
		return errors.Join(drainErr, r.cleanupErr)
	case <-ctx.Done():
		return errors.Join(drainErr, fmt.Errorf("close admin runtime: resource cleanup: %w", ctx.Err()))
	}
}

func (r *AdminAPIRuntime) quiesced() bool {
	r.stateMu.Lock()

	httpQuiesced, backgroundDone := r.httpQuiesced, r.backgroundDone
	closedBeforeStart := r.closing && !r.started
	r.stateMu.Unlock()

	// Start 진입을 닫은 미기동 runtime에는 join할 HTTP·background 작업이 없다.
	if closedBeforeStart {
		return true
	}

	if !httpQuiesced {
		return false
	}

	if backgroundDone == nil {
		return true
	}

	select {
	case <-backgroundDone:
		return true
	default:
		return false
	}
}
