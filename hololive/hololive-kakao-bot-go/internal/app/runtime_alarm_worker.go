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

package app

import (
	"context"
	"log/slog"
	"net/http"
	"sync"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/lifecycle"
)

type AlarmWorkerRuntime struct {
	Config *config.Config
	Logger *slog.Logger

	Scheduler        runtimeAlarmScheduler
	ConfigSubscriber *configsub.Subscriber
	ServerAddr       string
	HttpServer       *http.Server

	schedulerMu     sync.Mutex
	schedulerCancel context.CancelFunc
	lifecycle.Managed
}

func (r *AlarmWorkerRuntime) setAlarmSchedulerCancel(cancel context.CancelFunc) {
	if cancel == nil {
		return
	}

	r.schedulerMu.Lock()
	prevCancel := r.schedulerCancel
	r.schedulerCancel = cancel
	r.schedulerMu.Unlock()

	if prevCancel != nil {
		prevCancel()
	}
}

func (r *AlarmWorkerRuntime) clearAlarmSchedulerCancel() bool {
	r.schedulerMu.Lock()
	cancel := r.schedulerCancel
	r.schedulerCancel = nil
	r.schedulerMu.Unlock()

	if cancel != nil {
		cancel()
		return true
	}

	return false
}
