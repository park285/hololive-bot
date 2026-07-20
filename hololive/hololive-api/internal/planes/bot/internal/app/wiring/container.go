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

package wiring

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot"
)

type BuildHooks struct {
	InitializeBotDependencies func(ctx context.Context, appConfig *config.Config, logger *slog.Logger) (*bot.Dependencies, func(), error)
}

type BuiltContainer struct {
	Cleanup func()
}

func BuildContainer(ctx context.Context, appConfig *config.Config, logger *slog.Logger, hooks BuildHooks) (*BuiltContainer, error) {
	if appConfig == nil {
		return nil, errors.New("config must not be nil")
	}
	if logger == nil {
		return nil, errors.New("logger must not be nil")
	}
	if hooks.InitializeBotDependencies == nil {
		return nil, errors.New("initialize bot dependencies hook must not be nil")
	}
	if ctx == nil {
		return nil, errors.New("context must not be nil")
	}

	_, cleanup, err := hooks.InitializeBotDependencies(ctx, appConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize dependencies: %w", err)
	}

	return &BuiltContainer{Cleanup: cleanup}, nil
}
