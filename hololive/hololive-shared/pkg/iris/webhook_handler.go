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

package iris

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/valkey-io/valkey-go"

	"park285/iris-client-go/dedup"
	"park285/iris-client-go/webhook"
)

// WebhookHandlerOptions: 기존 구조체 기반 옵션 — 하위 호환성 유지를 위해 보존합니다.
type WebhookHandlerOptions struct {
	WorkerCount    int
	QueueSize      int
	EnqueueTimeout time.Duration
	HandlerTimeout time.Duration
	RequireHTTP2   bool
}

// WebhookHandler: iris-client-go webhook.Handler를 감싸는 얇은 어댑터입니다.
// gin.Context.Handle과 http.Handler 양쪽을 지원합니다.
type WebhookHandler struct {
	handler *webhook.Handler
}

// NewWebhookHandler: 구조체 옵션을 함수형 옵션으로 변환하여 WebhookHandler를 생성합니다.
func NewWebhookHandler(
	token string,
	handler MessageHandler,
	cacheClient valkey.Client,
	logger *slog.Logger,
	options ...WebhookHandlerOptions,
) *WebhookHandler {
	var opts []webhook.HandlerOption
	if cacheClient != nil {
		opts = append(opts, webhook.WithDeduplicator(dedup.NewValkeyDeduplicator(cacheClient)))
	}
	if len(options) > 0 {
		o := options[0]
		if o.WorkerCount > 0 {
			opts = append(opts, webhook.WithWorkerCount(o.WorkerCount))
		}
		if o.QueueSize > 0 {
			opts = append(opts, webhook.WithQueueSize(o.QueueSize))
		}
		if o.EnqueueTimeout > 0 {
			opts = append(opts, webhook.WithEnqueueTimeout(o.EnqueueTimeout))
		}
		if o.HandlerTimeout > 0 {
			opts = append(opts, webhook.WithHandlerTimeout(o.HandlerTimeout))
		}
		if o.RequireHTTP2 {
			opts = append(opts, webhook.WithRequireHTTP2(true))
		}
	}
	h := webhook.NewHandler(context.Background(), token, handler, logger, opts...)
	return &WebhookHandler{handler: h}
}

// Handle: gin 라우터에 등록할 핸들러입니다.
func (w *WebhookHandler) Handle(c *gin.Context) {
	w.handler.ServeHTTP(c.Writer, c.Request)
}

// ServeHTTP: http.Handler 인터페이스를 구현합니다.
func (w *WebhookHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	w.handler.ServeHTTP(rw, r)
}

// Close: 백그라운드 워커를 종료합니다.
func (w *WebhookHandler) Close() error {
	return w.handler.Close()
}
