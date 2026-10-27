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

	"github.com/kapu/hololive-api/internal/readiness"
	sharedreadiness "github.com/kapu/hololive-shared/pkg/readiness"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

// buildLLMSchedulerRouter는 llm plane의 health·ready 응답만 연다. 구독·digest·trigger는 같은 프로세스의 bot plane과
// 운영 CLI(--rerun-notification)가 직접 호출하므로 HTTP 경로가 없다.
func buildLLMSchedulerRouter(
	ctx context.Context,
	logger *slog.Logger,
	apiKey string,
	readyProbe *sharedreadiness.Probe,
) (*gin.Engine, error) {
	options := sharedserver.RuntimeRouterOptions{APIKey: apiKey}

	if readyProbe != nil {
		options.InternalReadyResponder = readiness.GinHandler(ctx, readyProbe)
	}

	router, err := sharedserver.NewRuntimeRouter(ctx, logger, &options)
	if err != nil {
		return nil, fmt.Errorf("build llm scheduler router: %w", err)
	}

	return router, nil
}
