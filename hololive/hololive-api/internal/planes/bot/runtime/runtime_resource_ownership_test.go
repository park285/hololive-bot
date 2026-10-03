package botruntime

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
)

func TestBotRuntimeCloseContextWaitsForDurableSamplerBeforeFreeingResources(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered := make(chan struct{})
		canceled := make(chan struct{})
		release := make(chan struct{})
		sampler := workercontract.NewQueueSampler(func(ctx context.Context) (workercontract.QueueValues, error) {
			close(entered)
			<-ctx.Done()
			close(canceled)
			<-release

			return workercontract.QueueValues{}, ctx.Err()
		})
		r := &BotRuntime{durable: &durableRuntime{
			inboxSampler: sampler, outboxSampler: workercontract.NewQueueSampler(nil), maintenanceEvery: time.Hour,
		}}

		assertRuntimeCloseWaitsForTask(t, r, entered, canceled, release)
	})
}

func TestBotRuntimeCloseContextWaitsForCertificateReloadBeforeFreeingResources(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered := make(chan struct{})
		canceled := make(chan struct{})
		release := make(chan struct{})
		r := &BotRuntime{h3CertReloadStart: func(ctx context.Context) {
			close(entered)
			<-ctx.Done()
			close(canceled)
			<-release
		}}

		assertRuntimeCloseWaitsForTask(t, r, entered, canceled, release)
	})
}

func assertRuntimeCloseWaitsForTask(t *testing.T, r *BotRuntime, entered, canceled, release chan struct{}) {
	t.Helper()

	finish := sync.OnceFunc(func() { close(release) })
	defer finish()

	var cleanups atomic.Int64

	r.cleanup = func() error {
		cleanups.Add(1)

		return nil
	}
	r.Start(t.Context(), nil)
	<-entered

	drainCtx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()

	if err := r.CloseContext(drainCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CloseContext while task is alive = %v, want deadline", err)
	}

	if got := cleanups.Load(); got != 0 {
		t.Fatalf("cleanup while task is alive=%d want=0", got)
	}

	select {
	case <-canceled:
	default:
		t.Fatal("shutdown did not cancel the resource user")
	}

	finish()

	if err := r.CloseContext(t.Context()); err != nil {
		t.Fatalf("CloseContext after task exit: %v", err)
	}

	r.Close()

	if got := cleanups.Load(); got != 1 {
		t.Fatalf("cleanup after repeated Close=%d want=1", got)
	}
}

func TestBotRuntimeCloseContextContinuesOneCleanupAfterDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered := make(chan struct{})
		release := make(chan struct{})
		finish := sync.OnceFunc(func() { close(release) })

		defer finish()

		var cleanups atomic.Int64

		r := &BotRuntime{cleanup: func() error {
			cleanups.Add(1)
			close(entered)
			<-release

			return nil
		}}
		ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)

		defer cancel()

		if err := r.CloseContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("CloseContext blocked in resource cleanup = %v, want deadline", err)
		}

		<-entered

		closed := make(chan error, 1)

		go func() { closed <- r.CloseContext(t.Context()) }()

		synctest.Wait()

		if got := cleanups.Load(); got != 1 {
			t.Fatalf("concurrent cleanup attempts=%d want=1", got)
		}

		finish()

		if err := <-closed; err != nil {
			t.Fatal(err)
		}

		if got := cleanups.Load(); got != 1 {
			t.Fatalf("completed cleanup attempts=%d want=1", got)
		}
	})
}

func TestBotRuntimeCloseBeforeStartFreesResourcesOnceAndPreventsStart(t *testing.T) {
	var cleanups atomic.Int64

	r := &BotRuntime{
		cleanup: func() error {
			cleanups.Add(1)

			return nil
		},
		h3CertReloadStart: func(context.Context) {
			t.Error("closed runtime started a background task")
		},
	}

	if err := r.CloseContext(t.Context()); err != nil {
		t.Fatal(err)
	}

	r.Start(t.Context(), nil)
	r.Close()

	if got := cleanups.Load(); got != 1 {
		t.Fatalf("cleanup before Start=%d want=1", got)
	}
}

func TestBotRuntimeClosePreservesFatalAndResourceErrorsAfterTasksJoin(t *testing.T) {
	fatalErr := errors.New("fatal task failure")
	closeErr := errors.New("resource close failure")

	bot, err := orchestration.NewBot(&orchestration.Dependencies{
		Logger:         slog.New(slog.DiscardHandler),
		Client:         &stubIrisClient{},
		MessageAdapter: messaging.NewMessageAdapter("!", ""),
		Formatter:      formatter.NewResponseFormatter("!", nil),
		Cache:          &cachemocks.Client{WaitUntilReadyFunc: func(context.Context, time.Duration) error { return fatalErr }},
		Postgres:       &databasemocks.Client{}, Holodex: &holodexprovider.Service{},
		Alarm: testAlarmCRUD{}, Matcher: &matcher.Matcher{}, MembersData: &stubMemberDataProvider{},
	})
	if err != nil {
		t.Fatal(err)
	}

	r := &BotRuntime{Bot: bot, cleanup: func() error { return closeErr }}
	errCh := make(chan error, 1)

	r.Start(t.Context(), errCh)

	select {
	case err = <-errCh:
		if !errors.Is(err, fatalErr) {
			t.Fatalf("background startup error=%v want fatal cause", err)
		}
	case <-time.After(time.Second):
		t.Fatal("fatal startup error was not reported")
	}

	err = r.CloseContext(t.Context())
	if !errors.Is(err, fatalErr) || !errors.Is(err, closeErr) {
		t.Fatalf("CloseContext error=%v want both fatal and resource causes", err)
	}
}

func TestBotRuntimeCertificateTaskPanicIsReportedAndRetainedAfterJoin(t *testing.T) {
	cause := errors.New("certificate task panic")
	runtime := &BotRuntime{h3CertReloadStart: func(context.Context) { panic(cause) }}
	errCh := make(chan error, 1)

	runtime.Start(t.Context(), errCh)

	select {
	case err := <-errCh:
		if !errors.Is(err, cause) {
			t.Fatalf("certificate task error=%v want panic cause", err)
		}
	case <-time.After(time.Second):
		t.Fatal("certificate task panic was not reported")
	}

	if err := runtime.CloseContext(t.Context()); !errors.Is(err, cause) {
		t.Fatalf("CloseContext error=%v want retained panic cause", err)
	}
}
