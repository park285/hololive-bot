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
	"strings"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/health"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
)

// ProvideAPIServer: 관리자용 HTTP 서버 인스턴스를 생성합니다.
func ProvideAPIServer(addr string, handler http.Handler, operation string) *http.Server {
	if handler == nil {
		handler = http.NotFoundHandler()
	}
	if strings.TrimSpace(operation) == "" {
		operation = runtimeHTTPServerOperationName(streamIngesterRuntimeName)
	}

	return &http.Server{
		Addr:              addr,
		Handler:           sharedserver.WrapH2C(otelhttp.NewHandler(handler, operation)),
		ReadHeaderTimeout: constants.ServerTimeout.ReadHeader,
		ReadTimeout:       constants.ServerTimeout.Read,
		WriteTimeout:      constants.ServerTimeout.Write,
		IdleTimeout:       constants.ServerTimeout.Idle,
		MaxHeaderBytes:    constants.ServerTimeout.MaxHeaderBytes,
	}
}

// ProvideHealthOnlyRouter: health + metrics 엔드포인트만 제공하는 최소 라우터.
func ProvideHealthOnlyRouter(ctx context.Context, logger *slog.Logger, readiness *ingestionReadinessState) (*gin.Engine, error) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	if err := router.SetTrustedProxies(constants.ServerConfig.TrustedProxies); err != nil {
		return nil, fmt.Errorf("set trusted proxies: %w", err)
	}
	router.TrustedPlatform = gin.PlatformCloudflare

	router.Use(gin.Recovery())
	router.Use(gzip.Gzip(gzip.DefaultCompression)) // 응답 압축 (HTTP/2 호환)
	sharedserver.ApplyBaseMiddleware(router, ctx, logger, sharedserver.BaseMiddlewareOptions{
		SkipLogPaths: []string{
			"/health",
			"/ready",
			"/metrics",
		},
	})

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, health.Get())
	})
	router.GET("/ready", func(c *gin.Context) {
		statusCode, payload := readiness.response()
		c.JSON(statusCode, payload)
	})
	router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	return router, nil
}
