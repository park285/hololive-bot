package iris

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	sharedirisx "github.com/park285/llm-kakao-bots/shared-go/pkg/irisx"
)

type blockingMessageHandler struct {
	started chan struct{}
	block   chan struct{}
}

func (h *blockingMessageHandler) HandleMessage(_ context.Context, _ *Message) {
	select {
	case h.started <- struct{}{}:
	default:
	}
	<-h.block
}

type countingBlockOnceMessageHandler struct {
	started chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

type noopMessageHandler struct{}

func (h *noopMessageHandler) HandleMessage(_ context.Context, _ *Message) {}

func (h *countingBlockOnceMessageHandler) HandleMessage(_ context.Context, _ *Message) {
	call := h.calls.Add(1)
	if call == 1 {
		select {
		case h.started <- struct{}{}:
		default:
		}
		<-h.release
	}
}

func TestWebhookHandler_BackpressureReturns503(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	token := "webhook-token"
	handlerImpl := &blockingMessageHandler{
		started: make(chan struct{}, 1),
		block:   make(chan struct{}),
	}
	webhookHandler := NewWebhookHandler(
		token,
		handlerImpl,
		nil,
		nil,
		WebhookHandlerOptions{
			WorkerCount:    1,
			QueueSize:      1,
			EnqueueTimeout: 10 * time.Millisecond,
			HandlerTimeout: 1 * time.Second,
		},
	)

	router := gin.New()
	router.POST(sharedirisx.PathWebhook, webhookHandler.Handle)

	requestBody := `{"text":"hello","room":"room-1","sender":"tester","userId":"user-1","threadId":"thread-1"}`
	doRequest := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, sharedirisx.PathWebhook, strings.NewReader(requestBody))
		req.Header.Set(sharedirisx.HeaderIrisToken, token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	first := doRequest()
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d", first.Code, http.StatusOK)
	}

	select {
	case <-handlerImpl.started:
	case <-time.After(1 * time.Second):
		t.Fatalf("worker did not start in time")
	}

	second := doRequest()
	if second.Code != http.StatusOK {
		t.Fatalf("second status = %d, want %d", second.Code, http.StatusOK)
	}

	third := doRequest()
	if third.Code != http.StatusServiceUnavailable {
		t.Fatalf("third status = %d, want %d", third.Code, http.StatusServiceUnavailable)
	}

	close(handlerImpl.block)
	if err := webhookHandler.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestWebhookHandler_CloseDrainsQueueAndRejectsNewTasks(t *testing.T) {
	t.Parallel()

	handlerImpl := &countingBlockOnceMessageHandler{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	webhookHandler := NewWebhookHandler(
		"token",
		handlerImpl,
		nil,
		nil,
		WebhookHandlerOptions{
			WorkerCount:    1,
			QueueSize:      2,
			EnqueueTimeout: 20 * time.Millisecond,
			HandlerTimeout: 1 * time.Second,
		},
	)

	task := webhookTask{
		ctx: context.Background(),
		msg: &Message{Msg: "msg"},
	}
	if err := webhookHandler.enqueue(task); err != nil {
		t.Fatalf("first enqueue() error = %v", err)
	}
	if err := webhookHandler.enqueue(task); err != nil {
		t.Fatalf("second enqueue() error = %v", err)
	}

	select {
	case <-handlerImpl.started:
	case <-time.After(1 * time.Second):
		t.Fatalf("worker did not start in time")
	}

	done := make(chan error, 1)
	go func() {
		done <- webhookHandler.Close()
	}()

	select {
	case err := <-done:
		t.Fatalf("Close() returned early: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(handlerImpl.release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("Close() did not return in time")
	}

	if got := handlerImpl.calls.Load(); got != 2 {
		t.Fatalf("handled calls = %d, want %d", got, 2)
	}

	if err := webhookHandler.enqueue(task); !errors.Is(err, errWebhookClosed) {
		t.Fatalf("enqueue() after Close error = %v, want %v", err, errWebhookClosed)
	}
}

func TestWebhookHandler_EnqueueCloseRaceSafe(t *testing.T) {
	t.Parallel()

	handlerImpl := &blockingMessageHandler{
		started: make(chan struct{}, 1),
		block:   make(chan struct{}),
	}
	webhookHandler := NewWebhookHandler(
		"token",
		handlerImpl,
		nil,
		nil,
		WebhookHandlerOptions{
			WorkerCount:    1,
			QueueSize:      1,
			EnqueueTimeout: 30 * time.Millisecond,
			HandlerTimeout: 1 * time.Second,
		},
	)

	task := webhookTask{
		ctx: context.Background(),
		msg: &Message{Msg: "msg"},
	}
	if err := webhookHandler.enqueue(task); err != nil {
		t.Fatalf("first enqueue() error = %v", err)
	}
	select {
	case <-handlerImpl.started:
	case <-time.After(1 * time.Second):
		t.Fatalf("worker did not start in time")
	}
	if err := webhookHandler.enqueue(task); err != nil {
		t.Fatalf("second enqueue() error = %v", err)
	}

	enqueueErrCh := make(chan error, 1)
	go func() {
		enqueueErrCh <- webhookHandler.enqueue(task)
	}()

	closeErrCh := make(chan error, 1)
	go func() {
		closeErrCh <- webhookHandler.Close()
	}()

	select {
	case err := <-enqueueErrCh:
		if !errors.Is(err, errWebhookQueueFull) && !errors.Is(err, errWebhookEnqueueTimeout) && !errors.Is(err, errWebhookClosed) {
			t.Fatalf("enqueue() race error = %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("enqueue() race did not return in time")
	}

	close(handlerImpl.block)

	select {
	case err := <-closeErrCh:
		if err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("Close() did not return in time")
	}

	if err := webhookHandler.enqueue(task); !errors.Is(err, errWebhookClosed) {
		t.Fatalf("enqueue() after Close error = %v, want %v", err, errWebhookClosed)
	}
}

func TestWebhookHandler_RequireHTTP2(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	token := "webhook-token"
	webhookHandler := NewWebhookHandler(
		token,
		&noopMessageHandler{},
		nil,
		nil,
		WebhookHandlerOptions{
			WorkerCount:    1,
			QueueSize:      8,
			EnqueueTimeout: 10 * time.Millisecond,
			HandlerTimeout: 1 * time.Second,
			RequireHTTP2:   true,
		},
	)
	t.Cleanup(func() {
		_ = webhookHandler.Close()
	})

	router := gin.New()
	router.POST(sharedirisx.PathWebhook, webhookHandler.Handle)

	requestBody := `{"text":"!도움","room":"room-1","sender":"tester","userId":"user-1","threadId":""}`

	http11Req := httptest.NewRequest(http.MethodPost, sharedirisx.PathWebhook, strings.NewReader(requestBody))
	http11Req.Header.Set(sharedirisx.HeaderIrisToken, token)
	http11Rec := httptest.NewRecorder()
	router.ServeHTTP(http11Rec, http11Req)
	if http11Rec.Code != http.StatusHTTPVersionNotSupported {
		t.Fatalf("HTTP/1.1 status = %d, want %d", http11Rec.Code, http.StatusHTTPVersionNotSupported)
	}

	http2Req := httptest.NewRequest(http.MethodPost, sharedirisx.PathWebhook, strings.NewReader(requestBody))
	http2Req.Header.Set(sharedirisx.HeaderIrisToken, token)
	http2Req.Proto = "HTTP/2.0"
	http2Req.ProtoMajor = 2
	http2Req.ProtoMinor = 0
	http2Rec := httptest.NewRecorder()
	router.ServeHTTP(http2Rec, http2Req)
	if http2Rec.Code != http.StatusOK {
		t.Fatalf("HTTP/2 status = %d, want %d", http2Rec.Code, http.StatusOK)
	}
}
