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

package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
)

const (
	irisReadyTimeout       = 10 * time.Minute
	irisReadyRetryInterval = 2 * time.Second
	irisReadyPingTimeout   = 3 * time.Second
)

type BotLifecycle struct {
	logger      *slog.Logger
	cache       CacheReadiness
	irisClient  IrisPinger
	irisBaseURL string
}

func NewBotLifecycle(logger *slog.Logger, cacheClient CacheReadiness, irisClient IrisPinger, irisBaseURL string) *BotLifecycle {
	return &BotLifecycle{
		logger:      logger,
		cache:       cacheClient,
		irisClient:  irisClient,
		irisBaseURL: irisBaseURL,
	}
}

// Start는 Valkey 미구성·준비 실패만 기동 오류로 돌려주고, 그 뒤에는 ctx가 끝날 때까지 돌아오지 않는다. Iris 준비
// 확인 실패는 기동을 막지 않는다. 그동안 기록된 응답의 발송 재시도는 reply outbox가 맡는다.
func (l *BotLifecycle) Start(ctx context.Context) error {
	l.logInfo("Starting Hololive KakaoTalk Bot...")

	if l.cache == nil {
		return errors.New("start bot: cache is not configured")
	}

	if err := l.cache.WaitUntilReady(ctx, constants.ValkeyConfig.ReadyTimeout); err != nil {
		return fmt.Errorf("start bot: valkey connection timeout: %w", err)
	}

	l.logInfo("Valkey connected")

	if err := l.WaitUntilIrisReady(ctx, irisReadyTimeout, irisReadyRetryInterval, irisReadyPingTimeout); err != nil {
		l.logWarn(
			"Iris server not ready at startup; continuing, reply outbox retries delivery",
			slog.String("base_url", l.irisBaseURL),
			slog.Any("error", err),
		)
	} else {
		l.logInfo("Iris server connected")
	}

	l.logInfo("Bot started successfully")

	<-ctx.Done()
	l.logInfo("Context canceled, shutting down...")

	return fmt.Errorf("start bot: context canceled: %w", ctx.Err())
}

func (l *BotLifecycle) Shutdown(_ context.Context) error {
	l.logInfo("Shutting down bot...")
	l.logInfo("Bot shutdown complete")

	return nil
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
