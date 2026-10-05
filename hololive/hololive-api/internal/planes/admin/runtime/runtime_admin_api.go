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

	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-api/internal/service/acl"
	"github.com/kapu/hololive-shared/pkg/constants"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

type photoSyncTask interface {
	Start(ctx context.Context)
}

type AdminAPIRuntime struct {
	lifecycle.Managed

	Config *apiconfig.AdminPlaneConfig
	Logger *slog.Logger

	ServerAddr  string
	HTTPServers *sharedserver.RuntimeHTTPServers
	PhotoSync   photoSyncTask

	// ACL은 관리 API가 변경하는 인스턴스다. 같은 프로세스의 봇 plane이 Follow로 추종한다.
	ACL *acl.Service

	stateMu     sync.Mutex
	started     bool
	closing     bool
	photoCancel context.CancelFunc
	photoDone   chan struct{}

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

// Close는 nil outer runtime에서도 안전하며 등록된 자원 정리를 한 번만 실행한다.
func (r *AdminAPIRuntime) Close() {
	if r == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), constants.AppTimeout.Shutdown)
	defer cancel()

	if err := r.CloseContext(ctx); err != nil && r.Logger != nil {
		r.Logger.Error("admin_runtime_close_failed", slog.Any("error", err))
	}
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
			} else {
				r.Managed.Close()
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

	httpQuiesced, photoDone := r.httpQuiesced, r.photoDone
	r.stateMu.Unlock()

	if !httpQuiesced {
		return false
	}

	if photoDone == nil {
		return true
	}

	select {
	case <-photoDone:
		return true
	default:
		return false
	}
}
