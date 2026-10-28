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
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
)

type testWebhookCloser struct {
	calls int
	err   error
}

func TestShutdownWebhookAndDurabilityAlwaysStopsWorkersAndJoinsErrors(t *testing.T) {
	closeErr := errors.New("webhook close failed")
	webhookCloser := &testWebhookCloser{err: closeErr}
	workerCtx, cancelWorker := context.WithCancel(t.Context())
	durable := &durableRuntime{cancel: cancelWorker}

	workerDone := make(chan struct{})

	durable.wg.Go(func() {
		<-workerCtx.Done()
		close(workerDone)
	})

	runtime := &BotRuntime{webhookHandlerCloser: webhookCloser, durable: durable}
	shutdownCtx, cancelShutdown := context.WithCancel(t.Context())
	cancelShutdown()

	err := runtime.shutdownWebhookAndDurability(shutdownCtx)
	if !errors.Is(err, closeErr) {
		t.Fatalf("joined shutdown error = %v", err)
	}

	select {
	case <-workerDone:
	case <-time.After(time.Second):
		t.Fatal("durable worker was not stopped")
	}
}

func TestDurableStopReturnsAtDeadlineWhenWorkerDoesNotExit(t *testing.T) {
	canceled := make(chan struct{})
	r := &durableRuntime{cancel: func() { close(canceled) }}
	r.wg.Add(1)

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)

	defer cancel()

	started := time.Now()
	err := r.Stop(ctx)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop() error = %v", err)
	}

	if time.Since(started) > time.Second {
		t.Fatal("Stop() did not honor its deadline")
	}

	select {
	case <-canceled:
	default:
		t.Fatal("Stop() did not cancel before waiting")
	}

	r.wg.Done()
}

func (c *testWebhookCloser) CloseContext(context.Context) error {
	c.calls++
	return c.err
}

func TestBotRuntimeShutdown_ClosesWebhookHandler(t *testing.T) {
	webhookCloser := &testWebhookCloser{}
	runtime := &BotRuntime{webhookHandlerCloser: webhookCloser}

	if err := runtime.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	if webhookCloser.calls != 1 {
		t.Fatalf("webhook CloseContext calls = %d, want %d", webhookCloser.calls, 1)
	}
}

// shutdownOrderLog는 종료 단계가 남기는 로그 메시지를 기록 순서대로 모은다. BotRuntime의 단계와 실제 Bot lifecycle이
// 같은 logger를 쓰므로 세 단계의 실행 순서를 한 줄로 확인할 수 있다.
type shutdownOrderLog struct {
	mu       sync.Mutex
	messages []string
}

func (*shutdownOrderLog) Enabled(context.Context, slog.Level) bool { return true }

func (l *shutdownOrderLog) Handle(_ context.Context, record slog.Record) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.messages = append(l.messages, record.Message)

	return nil
}

func (l *shutdownOrderLog) WithAttrs([]slog.Attr) slog.Handler { return l }

func (l *shutdownOrderLog) WithGroup(string) slog.Handler { return l }

func (l *shutdownOrderLog) indexOf(t *testing.T, message string) int {
	t.Helper()

	l.mu.Lock()
	defer l.mu.Unlock()

	index := slices.Index(l.messages, message)
	if index < 0 {
		t.Fatalf("log %q missing from %q", message, l.messages)
	}

	return index
}

func TestBotRuntimeShutdownRunsHTTPWebhookBotInOrderAndContinuesAfterErrors(t *testing.T) {
	logs := &shutdownOrderLog{}
	logger := slog.New(logs)
	webhookErr := errors.New("webhook close failed")
	webhookCloser := &testWebhookCloser{err: webhookErr}
	runtime := &BotRuntime{Bot: newShutdownOrderBot(t, logger), Logger: logger, webhookHandlerCloser: webhookCloser}

	// 처리 중인 요청을 남긴 채 취소된 context로 종료해 HTTP 단계가 반드시 실패하게 한다.
	finishRequest, responseDone := startBlockedShortLinkRequest(t, runtime)

	shutdownCtx, cancel := context.WithCancel(t.Context())
	cancel()

	err := runtime.Shutdown(shutdownCtx)
	if !errors.Is(err, context.Canceled) || !errors.Is(err, webhookErr) {
		t.Fatalf("Shutdown() error = %v, want HTTP cancellation and webhook error joined", err)
	}

	if webhookCloser.calls != 1 {
		t.Fatalf("webhook CloseContext calls = %d, want 1", webhookCloser.calls)
	}

	httpStage := logs.indexOf(t, "HTTP server shutdown error")
	webhookStage := logs.indexOf(t, "Iris webhook handler shutdown error")
	botStage := logs.indexOf(t, "Shutting down bot...")

	if httpStage >= webhookStage || webhookStage >= botStage {
		t.Fatalf("shutdown stage order http=%d webhook=%d bot=%d, want HTTP → webhook → bot", httpStage, webhookStage, botStage)
	}

	finishRequest()

	if err := <-responseDone; err != nil {
		t.Fatalf("in-flight request: %v", err)
	}
}

// newShutdownOrderBot은 종료 로그를 logger로 남기는 실제 orchestration.Bot을 만든다.
func newShutdownOrderBot(t *testing.T, logger *slog.Logger) *orchestration.Bot {
	t.Helper()

	bot, err := orchestration.NewBot(&orchestration.Dependencies{
		Logger: logger, Client: &stubIrisClient{}, ReplyOutbox: dispatchingReplyOutbox{client: &stubIrisClient{}}, MessageAdapter: messaging.NewMessageAdapter("!", ""),
		Formatter: formatter.NewResponseFormatter("!", nil), Cache: cachemocks.NewLenientClient(), Postgres: &databasemocks.Client{},
		Holodex: &holodexprovider.Service{}, Alarm: testAlarmCRUD{}, Matcher: &matcher.Matcher{}, MembersData: &stubMemberDataProvider{},
	})
	if err != nil {
		t.Fatal(err)
	}

	return bot
}

// startBlockedShortLinkRequest는 runtime의 ShortLinkServer에 처리 중인 요청 하나를 남긴다. 반환한 finish가 handler를
// 풀고, responseDone이 그 요청의 결과를 전달한다.
func startBlockedShortLinkRequest(t *testing.T, runtime *BotRuntime) (finish func(), responseDone <-chan error) {
	t.Helper()

	entered := make(chan struct{})
	release := make(chan struct{})

	finish = sync.OnceFunc(func() { close(release) })

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))

	runtime.ShortLinkServer = server.Config
	runtime.prepareHTTPHandlers()
	server.Start()
	t.Cleanup(server.Close)
	// Cleanup은 역순으로 실행되므로 handler를 먼저 풀어 server.Close가 처리 중인 요청을 기다리다 멈추지 않게 한다.
	t.Cleanup(finish)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)

	go func() {
		done <- performRuntimeTestRequest(server.Client(), req)
	}()

	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("HTTP handler did not start")
	}

	return finish, done
}
