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
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/service/configsub"

	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
)

type runtimeAlarmScheduler interface {
	Start(ctx context.Context)
}

// BotRuntime: 봇 애플리케이션의 전체 실행 환경 및 상태를 관리하는 구조체
type BotRuntime struct {
	Config *config.Config
	Logger *slog.Logger

	Bot            *bot.Bot
	AlarmScheduler runtimeAlarmScheduler // Alarm runtime scheduler

	ConfigSubscriber *configsub.Subscriber // Valkey Pub/Sub 설정 구독자

	ServerAddr string
	HttpServer *http.Server

	webhookHandlerCloser interface{ Close() error }
	alarmSchedulerMu     sync.Mutex
	alarmSchedulerCancel context.CancelFunc
	cleanup              func()
}

// Close - 런타임 리소스 정리 (DB, 캐시 연결 해제)
func (r *BotRuntime) Close() {
	if r != nil && r.cleanup != nil {
		r.cleanup()
	}
}

// BuildRuntime: 설정과 로거를 기반으로 봇 런타임 환경을 구성하고 모든 의존성을 초기화합니다.
func BuildRuntime(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*BotRuntime, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config must not be nil")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger must not be nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	runtime, cleanup, err := InitializeBotRuntime(ctx, cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize runtime: %w", err)
	}
	runtime.cleanup = cleanup

	return runtime, nil
}
