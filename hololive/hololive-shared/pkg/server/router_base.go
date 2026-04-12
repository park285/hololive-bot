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

package server

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/health"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
)

type BaseMiddlewareOptions struct {
	SkipLogPaths []string
	MaxBodyBytes int64
}

func ApplyBaseMiddleware(router *gin.Engine, ctx context.Context, logger *slog.Logger, opts BaseMiddlewareOptions) {
	if router == nil {
		return
	}

	maxBodyBytes := opts.MaxBodyBytes
	if maxBodyBytes <= 0 {
		maxBodyBytes = constants.ServerConfig.MaxBodyBytes
	}

	router.Use(middleware.LoggerMiddleware(ctx, logger, opts.SkipLogPaths...))
	router.Use(middleware.SecurityHeadersMiddleware())
	router.Use(middleware.RequestIDMiddleware())
	router.Use(middleware.MaxBodySizeMiddleware(maxBodyBytes))
}

func RegisterHealthRoutes(router gin.IRoutes) {
	if router == nil {
		return
	}

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, health.Get())
	})
	router.GET("/ready", func(c *gin.Context) {
		c.JSON(http.StatusOK, health.Get())
	})
}
