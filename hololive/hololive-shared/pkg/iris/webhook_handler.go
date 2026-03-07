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
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	sharedirisx "github.com/park285/llm-kakao-bots/shared-go/pkg/irisx"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/util"
)

const (
	defaultWebhookWorkerCount    = 16
	defaultWebhookQueueSize      = 1000
	defaultWebhookEnqueueTimeout = 50 * time.Millisecond
	defaultWebhookHandlerTimeout = 30 * time.Second
)

var (
	errWebhookQueueFull      = errors.New("iris webhook queue full")
	errWebhookEnqueueTimeout = errors.New("iris webhook enqueue timeout")
	errWebhookClosed         = errors.New("iris webhook handler closed")
)

var (
	metricsInitOnce             sync.Once
	webhookRequestTotal         *prometheus.CounterVec
	webhookEnqueueTotal         *prometheus.CounterVec
	webhookQueueDepth           prometheus.Gauge
	webhookQueueCapacity        prometheus.Gauge
	webhookWorkerConfigured     prometheus.Gauge
	webhookHandlerTimeoutSecond prometheus.Gauge
	webhookHandlerDuration      prometheus.Histogram
)

func initWebhookMetrics() {
	metricsInitOnce.Do(func() {
		webhookRequestTotal = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_iris_webhook_requests_total",
				Help: "Total Iris webhook requests by result.",
			},
			[]string{"result"},
		)

		webhookEnqueueTotal = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_iris_webhook_enqueue_total",
				Help: "Total Iris webhook enqueue attempts by result.",
			},
			[]string{"result"},
		)

		webhookQueueDepth = promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "hololive_iris_webhook_queue_depth",
				Help: "Current Iris webhook queue depth.",
			},
		)

		webhookQueueCapacity = promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "hololive_iris_webhook_queue_capacity",
				Help: "Configured Iris webhook queue capacity.",
			},
		)

		webhookWorkerConfigured = promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "hololive_iris_webhook_worker_count",
				Help: "Configured Iris webhook worker count.",
			},
		)

		webhookHandlerTimeoutSecond = promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "hololive_iris_webhook_handler_timeout_seconds",
				Help: "Configured Iris webhook handler timeout in seconds.",
			},
		)

		webhookHandlerDuration = promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "hololive_iris_webhook_handler_duration_seconds",
				Help:    "Iris webhook handler execution duration in seconds.",
				Buckets: prometheus.DefBuckets,
			},
		)
	})
}

// MessageHandler: webhook에서 수신한 메시지를 처리하는 인터페이스
type MessageHandler interface {
	HandleMessage(ctx context.Context, msg *Message)
}

type webhookTask struct {
	ctx context.Context
	msg *Message
}

type WebhookHandlerOptions struct {
	WorkerCount    int
	QueueSize      int
	EnqueueTimeout time.Duration
	HandlerTimeout time.Duration
	RequireHTTP2   bool
}

func (o WebhookHandlerOptions) normalized() WebhookHandlerOptions {
	result := o
	if result.WorkerCount <= 0 {
		result.WorkerCount = defaultWebhookWorkerCount
	}
	if result.QueueSize <= 0 {
		result.QueueSize = defaultWebhookQueueSize
	}
	if result.EnqueueTimeout <= 0 {
		result.EnqueueTimeout = defaultWebhookEnqueueTimeout
	}
	if result.HandlerTimeout <= 0 {
		result.HandlerTimeout = defaultWebhookHandlerTimeout
	}
	return result
}

type WebhookHandler struct {
	token       string // IRIS_WEBHOOK_TOKEN for inbound auth
	handler     MessageHandler
	cacheClient valkey.Client // for dedup (valkey-cache, NOT mq)
	logger      *slog.Logger
	options     WebhookHandlerOptions
	baseContext context.Context
	queue       chan webhookTask
	queueLock   sync.RWMutex
	workerWG    sync.WaitGroup
	closeOnce   sync.Once
	closed      bool
}

func NewWebhookHandler(
	token string,
	handler MessageHandler,
	cacheClient valkey.Client,
	logger *slog.Logger,
	options ...WebhookHandlerOptions,
) *WebhookHandler {
	if logger == nil {
		logger = slog.Default()
	}
	initWebhookMetrics()

	opt := WebhookHandlerOptions{}
	if len(options) > 0 {
		opt = options[0]
	}
	opt = opt.normalized()

	h := &WebhookHandler{
		token:       strings.TrimSpace(token),
		handler:     handler,
		cacheClient: cacheClient,
		logger:      logger,
		options:     opt,
		baseContext: context.Background(),
		queue:       make(chan webhookTask, opt.QueueSize),
	}

	if webhookQueueCapacity != nil {
		webhookQueueCapacity.Set(float64(opt.QueueSize))
	}
	if webhookWorkerConfigured != nil {
		webhookWorkerConfigured.Set(float64(opt.WorkerCount))
	}
	if webhookHandlerTimeoutSecond != nil {
		webhookHandlerTimeoutSecond.Set(opt.HandlerTimeout.Seconds())
	}
	h.observeQueueDepth()

	for i := 0; i < opt.WorkerCount; i++ {
		h.workerWG.Add(1)
		go h.worker(i)
	}

	logger.Info(
		"iris_webhook_workers_started",
		slog.Int("worker_count", opt.WorkerCount),
		slog.Int("queue_size", opt.QueueSize),
		slog.Int64("enqueue_timeout_ms", opt.EnqueueTimeout.Milliseconds()),
		slog.Int("handler_timeout_seconds", int(opt.HandlerTimeout.Seconds())),
	)

	return h
}

func (h *WebhookHandler) Close() error {
	h.closeOnce.Do(func() {
		h.queueLock.Lock()
		h.closed = true
		if h.queue != nil {
			close(h.queue)
		}
		h.queueLock.Unlock()

		h.workerWG.Wait()
		h.observeQueueDepth()

		if h.logger != nil {
			h.logger.Info("iris_webhook_workers_stopped")
		}
	})

	return nil
}

// Handle: POST /webhook/iris handler
// 1. Check method == POST, else 405
// 2. Validate X-Iris-Token header, else 401
// 3. Parse WebhookRequest body
// 4. Dedup: SET NX iris:msg:{X-Iris-Message-Id} in valkey-cache, TTL 60s
// 5. Enqueue to bounded queue
// 6. Return 200 on enqueue success, 503 on timeout/full
func (h *WebhookHandler) Handle(c *gin.Context) {
	// gin 라우팅이 POST로 제한되더라도, 외부 호출 경로이므로 방어적으로 체크합니다.
	if c.Request.Method != http.MethodPost {
		h.incRequest("method_not_allowed")
		c.Status(http.StatusMethodNotAllowed)
		return
	}

	if h.options.RequireHTTP2 && c.Request.ProtoMajor != 2 {
		h.logger.Warn(
			"iris_webhook_http2_required",
			slog.String("proto", c.Request.Proto),
			slog.Int("proto_major", c.Request.ProtoMajor),
			slog.Int("proto_minor", c.Request.ProtoMinor),
		)
		h.incRequest("http_version_not_supported")
		c.Status(http.StatusHTTPVersionNotSupported)
		return
	}

	if h.token == "" {
		h.logger.Error("iris_webhook_token_missing")
		h.incRequest("internal_error")
		c.Status(http.StatusInternalServerError)
		return
	}

	if c.GetHeader(sharedirisx.HeaderIrisToken) != h.token {
		h.incRequest("unauthorized")
		c.Status(http.StatusUnauthorized)
		return
	}

	var req WebhookRequest
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		h.logger.Warn("iris_webhook_invalid_json", slog.Any("error", err))
		h.incRequest("bad_request")
		c.Status(http.StatusBadRequest)
		return
	}

	// dedup: 요청 컨텍스트로 캐시 조회 (빠른 연산)
	if status, ok := h.checkDedup(c); ok {
		c.Status(status)
		return
	}

	if h.handler == nil {
		h.logger.Error("iris_webhook_handler_missing")
		h.incRequest("internal_error")
		c.Status(http.StatusInternalServerError)
		return
	}

	irisMsg := &Message{
		Msg:  req.Text,
		Room: req.Room,
		Sender: func() *string {
			s := req.Sender
			return new(s)
		}(),
		JSON: &MessageJSON{
			UserID: req.UserID,
			ChatID: req.Room,
		},
	}

	task := webhookTask{
		ctx: context.WithoutCancel(c.Request.Context()),
		msg: irisMsg,
	}
	if err := h.enqueue(task); err != nil {
		h.logger.Warn(
			"iris_webhook_enqueue_failed",
			slog.Any("error", err),
			slog.Int("queue_depth", len(h.queue)),
			slog.Int("queue_size", cap(h.queue)),
		)
		h.incRequest("backpressure")
		c.Status(http.StatusServiceUnavailable)
		return
	}

	h.incRequest("accepted")
	c.Status(http.StatusOK)
}

// checkDedup: 메시지 ID 기반 중복 요청 체크. 중복이면 (status, true) 반환.
func (h *WebhookHandler) checkDedup(c *gin.Context) (int, bool) {
	msgID := c.GetHeader(sharedirisx.HeaderIrisMessageID)
	if msgID == "" || h.cacheClient == nil {
		return 0, false
	}

	dedupKey := sharedirisx.DedupKey(msgID)
	ttl := constants.IrisWebhookDedupTTL
	if ttl <= 0 {
		ttl = sharedirisx.DefaultWebhookDedupTTL
	}

	cmd := h.cacheClient.B().Set().Key(dedupKey).Value("1").Nx().ExSeconds(int64(ttl.Seconds())).Build()
	resp := h.cacheClient.Do(c.Request.Context(), cmd)
	if util.IsValkeyNil(resp.Error()) {
		h.logger.Info("iris_webhook_dedup_skipped", slog.String("message_id", msgID))
		h.incRequest("dedup")
		return http.StatusOK, true
	}
	if resp.Error() != nil {
		h.logger.Error("iris_webhook_dedup_failed", slog.String("message_id", msgID), slog.Any("error", resp.Error()))
		h.incRequest("internal_error")
		return http.StatusInternalServerError, true
	}
	return 0, false
}

func (h *WebhookHandler) enqueue(task webhookTask) error {
	h.queueLock.RLock()
	defer h.queueLock.RUnlock()

	if h.closed || h.queue == nil {
		h.incEnqueue("closed")
		return errWebhookClosed
	}

	select {
	case h.queue <- task:
		h.incEnqueue("ok")
		h.observeQueueDepth()
		return nil
	default:
	}

	timer := time.NewTimer(h.options.EnqueueTimeout)
	defer timer.Stop()

	select {
	case h.queue <- task:
		h.incEnqueue("ok")
		h.observeQueueDepth()
		return nil
	case <-timer.C:
		if len(h.queue) >= cap(h.queue) {
			h.incEnqueue("queue_full")
			return errWebhookQueueFull
		}
		h.incEnqueue("timeout")
		return errWebhookEnqueueTimeout
	}
}

func (h *WebhookHandler) worker(index int) {
	defer h.workerWG.Done()

	for task := range h.queue {
		h.observeQueueDepth()
		start := time.Now()

		func() {
			defer func() {
				if recovered := recover(); recovered != nil && h.logger != nil {
					h.logger.Error(
						"iris_webhook_worker_panic_recovered",
						slog.Int("worker_index", index),
						slog.Any("panic", recovered),
					)
				}
			}()

			ctx := task.ctx
			if ctx == nil {
				ctx = h.baseContext
			}

			runCtx := ctx
			cancel := func() {}
			if h.options.HandlerTimeout > 0 {
				runCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), h.options.HandlerTimeout)
			}
			defer cancel()

			h.handler.HandleMessage(runCtx, task.msg)
		}()

		if webhookHandlerDuration != nil {
			webhookHandlerDuration.Observe(time.Since(start).Seconds())
		}
	}
}

func (h *WebhookHandler) observeQueueDepth() {
	if webhookQueueDepth != nil && h.queue != nil {
		webhookQueueDepth.Set(float64(len(h.queue)))
	}
}

func (h *WebhookHandler) incRequest(result string) {
	if webhookRequestTotal != nil {
		webhookRequestTotal.WithLabelValues(result).Inc()
	}
}

func (h *WebhookHandler) incEnqueue(result string) {
	if webhookEnqueueTotal != nil {
		webhookEnqueueTotal.WithLabelValues(result).Inc()
	}
}
