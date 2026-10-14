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

package runtime

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gin-gonic/gin"

	apiserver "github.com/kapu/hololive-api/internal/httpapi"
	"github.com/kapu/hololive-api/internal/readiness"
	sharedreadiness "github.com/kapu/hololive-shared/pkg/readiness"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

func buildTriggerRouter(
	ctx context.Context,
	logger *slog.Logger,
	triggerHandler *apiserver.TriggerHandler,
	apiKey string,
	readyProbe ...*sharedreadiness.Probe,
) (*gin.Engine, error) {
	var opts []func(*sharedserver.RuntimeRouterOptions)

	if probe := readiness.Pick(readyProbe...); probe != nil {
		opts = append(opts, func(o *sharedserver.RuntimeRouterOptions) {
			o.InternalReadyResponder = readiness.GinHandler(ctx, probe)
		})
	}

	router, err := apiserver.NewTriggerRuntimeRouter(ctx, logger, triggerHandler, apiKey, opts...)
	if err != nil {
		return nil, fmt.Errorf("build trigger router: %w", err)
	}

	return router, nil
}
