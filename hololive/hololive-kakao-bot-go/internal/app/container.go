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
	"errors"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/lifecycle"

	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
)

// Container: 애플리케이션의 모든 서비스와 의존성(Config, Logger, Services)을 관리하는 DI 컨테이너.
type Container struct {
	Config *config.Config
	Logger *slog.Logger

	botDeps *bot.Dependencies
	lifecycle.CleanupCloser
}

func (c *Container) Close() {
	if c == nil {
		return
	}

	c.CleanupCloser.Close()
}

// Build: 주어진 설정과 로거를 기반으로 애플리케이션 컨테이너를 구성하고 모든 의존성을 초기화합니다.
func Build(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*Container, error) {
	if cfg == nil {
		return nil, errors.New("config must not be nil")
	}

	if logger == nil {
		return nil, errors.New("logger must not be nil")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	// 의존성 주입 초기화
	deps, cleanup, err := InitializeBotDependencies(ctx, cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize dependencies: %w", err)
	}

	return &Container{
		Config:        cfg,
		Logger:        logger,
		botDeps:       deps,
		CleanupCloser: lifecycle.NewCleanupCloser(cleanup),
	}, nil
}
