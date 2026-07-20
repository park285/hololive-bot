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
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/park285/shared-go/pkg/runtime/lifecycle"

	appwiring "github.com/kapu/hololive-api/internal/planes/bot/internal/app/wiring"
)

type Container struct {
	Config *config.Config
	Logger *slog.Logger

	lifecycle.Managed
}

func Build(ctx context.Context, appConfig *config.Config, logger *slog.Logger) (*Container, error) {
	built, err := appwiring.BuildContainer(ctx, appConfig, logger, appwiring.BuildHooks{
		InitializeBotDependencies: InitializeBotDependencies,
	})
	if err != nil {
		return nil, err
	}

	return &Container{
		Config:  appConfig,
		Logger:  logger,
		Managed: lifecycle.NewManaged(built.Cleanup),
	}, nil
}
