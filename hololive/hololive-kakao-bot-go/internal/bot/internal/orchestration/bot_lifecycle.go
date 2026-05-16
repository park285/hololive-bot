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

package orchestration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"
)

type BotLifecycle struct {
	logger      *slog.Logger
	cache       cache.Client
	irisClient  irisClient
	irisBaseURL string
	stopCh      chan struct{}
	doneCh      chan struct{}
	doneOnce    sync.Once
	workerPool  *workerpool.Pool
	holodex     streamRuntime
	postgres    database.Client
}

func NewBotLifecycle(
	logger *slog.Logger,
	cacheSvc cache.Client,
	irisClient irisClient,
	irisBaseURL string,
	stopCh chan struct{},
	doneCh chan struct{},
	workerPool *workerpool.Pool,
	holodex streamRuntime,
	postgres database.Client,
) *BotLifecycle {
	return &BotLifecycle{
		logger:      logger,
		cache:       cacheSvc,
		irisClient:  irisClient,
		irisBaseURL: irisBaseURL,
		stopCh:      stopCh,
		doneCh:      doneCh,
		workerPool:  workerPool,
		holodex:     holodex,
		postgres:    postgres,
	}
}

func (l *BotLifecycle) Start(ctx context.Context) error {
	l.logInfo("Starting Hololive KakaoTalk Bot...")

	if l.cache == nil {
		return errors.New("start bot: cache is not configured")
	}

	if err := l.cache.WaitUntilReady(ctx, constants.ValkeyConfig.ReadyTimeout); err != nil {
		return fmt.Errorf("start bot: valkey connection timeout: %w", err)
	}

	l.logInfo("Valkey connected")

	if err := l.WaitUntilIrisReady(
		ctx,
		constants.IrisConnection.ReadyTimeout,
		constants.IrisConnection.RetryInterval,
		constants.IrisConnection.PingTimeout,
	); err != nil {
		l.logWarn(
			"Iris server not ready at startup; continuing in degraded mode",
			slog.String("base_url", l.irisBaseURL),
			slog.Any("error", err),
		)
	} else {
		l.logInfo("Iris server connected")
	}

	l.logInfo("Bot started successfully")

	select {
	case <-ctx.Done():
		l.logInfo("Context canceled, shutting down...")
		return fmt.Errorf("start bot: context canceled: %w", ctx.Err())
	case <-l.stopCh:
		l.logInfo("Stop signal received")
		return nil
	}
}

func (l *BotLifecycle) Shutdown(ctx context.Context) error {
	l.logInfo("Shutting down bot...")

	l.shutdownWorkerPool(ctx)
	l.stopHolodex()
	l.closeCache()
	l.closePostgres()
	l.closeDoneCh()

	l.logInfo("Bot shutdown complete")

	return nil
}

func (l *BotLifecycle) shutdownWorkerPool(ctx context.Context) {
	if l.workerPool != nil {
		if err := l.workerPool.ShutdownWait(ctx); err != nil {
			l.logWarn("Worker pool shutdown error", slog.Any("error", err))
		}
	}
}

func (l *BotLifecycle) stopHolodex() {
	if l.holodex != nil {
		l.holodex.Stop()
	}
}

func (l *BotLifecycle) closeCache() {
	if l.cache != nil {
		if err := l.cache.Close(); err != nil {
			l.logWarn("Error closing cache", slog.Any("error", err))
		}
	}
}

func (l *BotLifecycle) closePostgres() {
	if l.postgres != nil {
		if err := l.postgres.Close(); err != nil {
			l.logWarn("Error closing postgres", slog.Any("error", err))
		}
	}
}

func (l *BotLifecycle) closeDoneCh() {
	l.doneOnce.Do(func() {
		if l.doneCh != nil {
			close(l.doneCh)
		}
	})
}

func (l *BotLifecycle) logInfo(msg string, attrs ...slog.Attr) {
	if l == nil || l.logger == nil {
		return
	}

	args := make([]any, 0, len(attrs))
	for _, attr := range attrs {
		args = append(args, attr)
	}

	l.logger.Info(msg, args...)
}

func (l *BotLifecycle) logWarn(msg string, attrs ...slog.Attr) {
	if l == nil || l.logger == nil {
		return
	}

	args := make([]any, 0, len(attrs))
	for _, attr := range attrs {
		args = append(args, attr)
	}

	l.logger.Warn(msg, args...)
}
