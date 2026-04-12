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
	"strings"

	"github.com/gin-gonic/gin"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
)

func buildHealthOnlyRouter(ctx context.Context, logger *slog.Logger, apiKey string) (*gin.Engine, error) {
	return sharedserver.NewRuntimeRouter(ctx, logger, sharedserver.RuntimeRouterOptions{
		APIKey: apiKey,
	})
}

func buildTriggerRouter(
	ctx context.Context,
	logger *slog.Logger,
	triggerHandler *sharedserver.TriggerHandler,
	apiKey string,
) (*gin.Engine, error) {
	return sharedserver.NewRuntimeRouter(ctx, logger, sharedserver.RuntimeRouterOptions{
		APIKey: apiKey,
		RegisterRoutes: func(router *gin.Engine) error {
			if triggerHandler == nil {
				return nil
			}
			if strings.TrimSpace(apiKey) == "" {
				return fmt.Errorf("API_SECRET_KEY required")
			}
			triggerHandler.RegisterInternalRoutesWithAuth(router.Group(""), apiKey)
			return nil
		},
	})
}
