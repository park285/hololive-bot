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
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/park285/shared-go/v2/pkg/runtime/bootstrap"
	"github.com/park285/shared-go/v2/pkg/workercontract"
	"github.com/quic-go/quic-go/http3"

	appbootstrap "github.com/kapu/hololive-api/internal/planes/bot/internal/app/bootstrap"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration"
	"github.com/kapu/hololive-api/internal/service/acl"
	"github.com/kapu/hololive-shared/pkg/config/settings"
)

type BotRuntime struct {
	Config *settings.Config
	Logger *slog.Logger

	Bot *orchestration.Bot

	// ACL은 봇 판정 인스턴스다. 같은 프로세스의 관리 plane 변경은 Follow로 받는다.
	ACL *acl.Service

	ServerAddr      string
	H3Server        *http3.Server
	ShortLinkServer *http.Server
	MetricsServer   *http.Server
	PprofServer     *http.Server

	h3CertReloadStart func(context.Context)

	webhookHandlerCloser interface{ CloseContext(context.Context) error }
	durable              *durableRuntime
	workerRegistry       *workercontract.Registry
	workerProfileChecker *workercontract.ProfileFileChecker

	lifecycleMu        sync.Mutex
	started            bool
	stopping           bool
	tasksCancel        context.CancelFunc
	tasksWG            sync.WaitGroup
	tasksDone          chan struct{}
	backgroundErr      error
	shutdownErr        error
	quiesced           atomic.Bool
	cleanup            func() error
	resourcesCloseOnce sync.Once
	resourcesDone      chan struct{}
	resourcesErr       error
	requestsMu         sync.Mutex
	requestsWG         sync.WaitGroup
	requestsDone       chan struct{}
	requestsClosing    bool
	httpHandlersOnce   sync.Once
}

func BuildRuntime(ctx context.Context, appConfig *settings.Config, logger *slog.Logger) (*BotRuntime, error) {
	ctx, err := bootstrap.NormalizeRuntimeBuildInputs(ctx, appConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("normalize runtime build inputs: %w", err)
	}

	infra, err := appbootstrap.InitBotInfrastructure(ctx, appConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize runtime: init bot infrastructure: %w", err)
	}

	runtime, err := buildBotRuntime(ctx, appConfig, logger, infra)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("failed to initialize runtime: build bot runtime: %w", err), infra.Cleanup())
	}

	runtime.cleanup = infra.Cleanup

	return runtime, nil
}

// Close는 nil outer runtime에서도 안전하며 등록된 자원 정리를 한 번만 실행한다.
func (r *BotRuntime) Close() {
	if r == nil {
		return
	}

	if err := r.CloseContext(context.Background()); err != nil {
		r.logError("bot runtime close failed", err)
	}
}
