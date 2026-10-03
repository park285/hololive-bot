package botruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
)

type observedBotReadinessCache struct {
	*cache.Service

	beforeWait func(context.Context)
	afterWait  func(context.Context)
	timeout    time.Duration
}

func (c *observedBotReadinessCache) WaitUntilReady(ctx context.Context, timeout time.Duration) error {
	c.beforeWait(ctx)

	if c.timeout > 0 {
		timeout = min(timeout, c.timeout)
	}

	err := c.Service.WaitUntilReady(ctx, timeout)
	if c.afterWait != nil {
		c.afterWait(ctx)
	}

	if err != nil {
		return fmt.Errorf("observe cache readiness: %w", err)
	}

	return nil
}

func TestBotRuntimeHealthyReadinessCancellationIsNormalShutdown(t *testing.T) {
	service := newBotReadinessCacheService(t)

	synctest.Test(t, func(t *testing.T) {
		entered := make(chan struct{})
		observed := &observedBotReadinessCache{
			Service: service,
			beforeWait: func(ctx context.Context) {
				close(entered)
				// 실제 캐시의 첫 PING 전에 종료하는 순서를 sleep 없이 고정한다.
				<-ctx.Done()
			},
		}
		runtime := newBotReadinessRuntime(t, observed)

		defer runtime.Close()

		runtime.Start(t.Context(), nil)
		<-entered

		if err := runtime.CloseContext(t.Context()); err != nil {
			t.Fatalf("healthy readiness cancellation became fatal: %v", err)
		}
	})
}

func TestBotRuntimeReadinessOwnTimeoutRemainsFatal(t *testing.T) {
	service := newBotReadinessCacheService(t)

	synctest.Test(t, func(t *testing.T) {
		contexts := make(chan context.Context, 1)
		observed := &observedBotReadinessCache{
			Service:    service,
			beforeWait: func(ctx context.Context) { contexts <- ctx },
			timeout:    10 * time.Millisecond,
		}
		runtime := newBotReadinessRuntime(t, observed)

		defer runtime.Close()

		errCh := make(chan error, 1)

		runtime.Start(t.Context(), errCh)

		runCtx := <-contexts
		err := <-errCh

		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("readiness own timeout=%v want fatal deadline", err)
		}

		if err := runCtx.Err(); err != nil {
			t.Fatalf("readiness own timeout canceled the runtime: %v", err)
		}

		if err := runtime.CloseContext(t.Context()); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("CloseContext lost readiness startup failure: %v", err)
		}
	})
}

func TestBotRuntimeReadinessCancellationRetainsNestedFatalCause(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fatalErr := errors.New("readiness fatal failure")
		entered := make(chan struct{})
		client := &cachemocks.Client{WaitUntilReadyFunc: func(ctx context.Context, _ time.Duration) error {
			close(entered)
			<-ctx.Done()

			return fmt.Errorf("readiness failed: %w", errors.Join(
				ctx.Err(), errors.Join(context.DeadlineExceeded, fmt.Errorf("nested readiness failure: %w", fatalErr)),
			))
		}}
		runtime := newBotReadinessRuntime(t, client)

		defer runtime.Close()

		runtime.Start(t.Context(), nil)
		<-entered

		err := runtime.CloseContext(t.Context())
		if !errors.Is(err, fatalErr) || !errors.Is(err, context.Canceled) || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("CloseContext lost nested startup causes: %v", err)
		}
	})
}

func TestBotRuntimeReadinessOwnTimeoutBeforeShutdownRemainsFatal(t *testing.T) {
	service := newBotReadinessCacheService(t)

	synctest.Test(t, func(t *testing.T) {
		contexts := make(chan context.Context, 1)
		expired := make(chan struct{})
		observed := &observedBotReadinessCache{
			Service:    service,
			beforeWait: func(ctx context.Context) { contexts <- ctx },
			afterWait: func(ctx context.Context) {
				close(expired)
				// 자체 timeout 이후 runtime의 오류 분류 직전에 종료를 요청한다.
				<-ctx.Done()
			},
			timeout: 10 * time.Millisecond,
		}
		runtime := newBotReadinessRuntime(t, observed)

		defer runtime.Close()

		runtime.Start(t.Context(), nil)
		<-contexts
		<-expired

		if err := runtime.CloseContext(t.Context()); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("CloseContext lost readiness timeout preceding shutdown: %v", err)
		}
	})
}

func newBotReadinessCacheService(t *testing.T) *cache.Service {
	t.Helper()

	mini := miniredis.RunT(t)

	port, err := strconv.Atoi(mini.Port())
	if err != nil {
		t.Fatal(err)
	}

	service, err := cache.NewCacheService(t.Context(), cache.Config{
		Host: mini.Host(), Port: port, DisableCache: true, ForceSingleClient: true,
	}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Errorf("close readiness cache: %v", err)
		}
	})

	return service
}

func newBotReadinessRuntime(t *testing.T, client cache.Client) *BotRuntime {
	t.Helper()

	logger := slog.New(slog.DiscardHandler)

	bot, err := orchestration.NewBot(&orchestration.Dependencies{
		Logger: logger, Client: &stubIrisClient{}, MessageAdapter: messaging.NewMessageAdapter("!", ""),
		Formatter: formatter.NewResponseFormatter("!", nil), Cache: client, Postgres: &databasemocks.Client{},
		Holodex: &holodexprovider.Service{}, Alarm: testAlarmCRUD{}, Matcher: &matcher.Matcher{}, MembersData: &stubMemberDataProvider{},
	})
	if err != nil {
		t.Fatal(err)
	}

	return &BotRuntime{Bot: bot, Logger: logger}
}
