package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"

	"github.com/kapu/hololive-shared/pkg/constants"
)

func TestRuntimeRunReturnsTerminalResultAfterCancelDrainAndClose(t *testing.T) {
	fatalErr := errors.New("listener failed")

	tests := []struct {
		name     string
		terminal error
	}{
		{name: "runtime fatal", terminal: fatalErr},
		{name: "clean terminal"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var (
				calls       []string
				runtimeDone <-chan struct{}
				logs        bytes.Buffer
			)

			group := &runtimeGroupStub{
				start: func(ctx context.Context, errCh chan<- error) {
					runtimeDone = ctx.Done()

					errCh <- test.terminal
				},
				// shutdown hook은 시한을 강제하는 goroutine에서 돌므로 FailNow 계열을 쓰지 않는다.
				shutdown: func(context.Context) error {
					select {
					case <-runtimeDone:
						calls = append(calls, "shutdown")
					default:
						t.Error("runtime context was not canceled before shutdown")
					}

					if logged := strings.Contains(logs.String(), "hololive-api runtime error"); logged != (test.terminal != nil) {
						t.Errorf("runtime error logged before drain = %t, want %t", logged, test.terminal != nil)
					}

					return nil
				},
			}
			runtime := runTestRuntime(group, func(context.Context) error {
				calls = append(calls, "close")

				return nil
			})

			runtime.Logger = slog.New(slog.NewTextHandler(&logs, nil))

			err := runtime.Run()

			if test.terminal == nil && err != nil {
				t.Fatalf("Run() error = %v, want nil", err)
			}

			if test.terminal != nil && !errors.Is(err, test.terminal) {
				t.Fatalf("Run() error = %v, want %v", err, test.terminal)
			}

			if want := []string{"shutdown", "close"}; !reflect.DeepEqual(calls, want) {
				t.Fatalf("Run() calls = %v, want %v", calls, want)
			}
		})
	}
}

func TestRuntimeRunReturnsNilAfterShutdownSignal(t *testing.T) {
	var logs bytes.Buffer

	shutdownCalled := false
	group := &runtimeGroupStub{
		start: func(context.Context, chan<- error) {
			// lifecycle.Run은 Start 전에 SIGTERM을 구독하므로 자기 프로세스에 보낸 신호가 테스트를 끝내지 않는다.
			if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
				t.Fatalf("send SIGTERM: %v", err)
			}
		},
		shutdown: func(context.Context) error {
			shutdownCalled = true

			return nil
		},
	}
	runtime := runTestRuntime(group, func(context.Context) error { return nil })

	runtime.Logger = slog.New(slog.NewTextHandler(&logs, nil))

	if err := runtime.Run(); err != nil {
		t.Fatalf("Run() error = %v, want nil after SIGTERM", err)
	}

	if !shutdownCalled {
		t.Fatal("Run() did not drain runtime planes after SIGTERM")
	}

	for _, want := range []string{"Received shutdown signal", "signal=terminated", "hololive-api draining runtime planes"} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("logs = %q, want %q", logs.String(), want)
		}
	}
}

func TestRuntimeRunPreservesFatalDrainAndCloseErrors(t *testing.T) {
	fatalErr := errors.New("listener failed")
	drainErr := errors.New("admin drain failed")
	closeErr := errors.New("client close failed")
	group := &runtimeGroupStub{
		start:    func(_ context.Context, errCh chan<- error) { errCh <- fatalErr },
		shutdown: func(context.Context) error { return drainErr },
	}
	runtime := runTestRuntime(group, func(context.Context) error { return closeErr })

	err := runtime.Run()

	for _, want := range []error{fatalErr, drainErr, closeErr} {
		if !errors.Is(err, want) {
			t.Fatalf("Run() error = %v, want %v identifiable", err, want)
		}
	}
}

// bootstrap은 Run 뒤 Close를 항상 호출하므로 완료된 회수를 다시 실행하지 않아야 한다.
func TestRuntimeCloseAfterRunDoesNotRepeatCleanup(t *testing.T) {
	closeCalls := 0
	group := &runtimeGroupStub{start: func(_ context.Context, errCh chan<- error) { errCh <- nil }}
	runtime := runTestRuntime(group, func(context.Context) error {
		closeCalls++

		return nil
	})

	if err := runtime.Run(); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	runtime.Close()

	if closeCalls != 1 {
		t.Fatalf("close step calls = %d, want 1", closeCalls)
	}
}

func TestRuntimeStopSplitsDrainAndCloseBudgetWithoutCloseRejoin(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var drainElapsed time.Duration

		release := make(chan struct{})
		start := time.Now()
		group := &runtimeGroupStub{
			start: func(_ context.Context, errCh chan<- error) { errCh <- nil },
			shutdown: func(ctx context.Context) error {
				<-ctx.Done()

				drainElapsed = time.Since(start)

				return ctx.Err()
			},
		}
		runtime := runTestRuntime(group, func(context.Context) error {
			<-release

			return nil
		})

		err := runLifecycleWithoutSignals(t, runtime)

		// bootstrap이 Run 뒤에 부르는 Close가 미완료 회수에 다시 합류하면 stop 상한을 넘긴다.
		runtime.Close()

		elapsed := time.Since(start)

		close(release)

		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("lifecycle error = %v, want stop deadline", err)
		}

		if drainElapsed != constants.AppTimeout.Shutdown {
			t.Fatalf("drain elapsed = %v, want %v", drainElapsed, constants.AppTimeout.Shutdown)
		}

		if elapsed != processStopTimeout {
			t.Fatalf("stop elapsed = %v, want close to use only the remaining budget up to %v", elapsed, processStopTimeout)
		}
	})
}

func TestRuntimeStopHasHardDeadlineWhenPlaneIgnoresCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fatalErr := errors.New("listener failed")
		release := make(chan struct{})
		start := time.Now()
		group := &runtimeGroupStub{
			start: func(_ context.Context, errCh chan<- error) { errCh <- fatalErr },
			shutdown: func(context.Context) error {
				<-release

				return nil
			},
		}

		var closeStarted atomic.Bool

		runtime := runTestRuntime(group, func(context.Context) error {
			closeStarted.Store(true)

			return nil
		})

		err := runLifecycleWithoutSignals(t, runtime)

		runtime.Close()

		elapsed := time.Since(start)

		// 시한 뒤에 끝난 drain이 프로세스 종료와 겹쳐 자원 회수를 새로 시작하면 안 된다.
		time.Sleep(5 * time.Second)
		close(release)
		synctest.Wait()

		if !errors.Is(err, fatalErr) || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("lifecycle error = %v, want fatal and stop deadline", err)
		}

		if elapsed != processStopTimeout {
			t.Fatalf("stop elapsed = %v, want hard stop deadline %v", elapsed, processStopTimeout)
		}

		if closeStarted.Load() {
			t.Fatal("abandoned drain started resource cleanup after the stop deadline")
		}
	})
}

func TestRuntimeStopDeadlineKeepsEarlierCloseStepFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stepErr := errors.New("bot client close failed")
		release := make(chan struct{})
		group := &runtimeGroupStub{start: func(_ context.Context, errCh chan<- error) { errCh <- nil }}
		runtime := runTestRuntime(group,
			func(context.Context) error { return stepErr },
			func(context.Context) error {
				<-release

				return nil
			},
		)

		err := runLifecycleWithoutSignals(t, runtime)

		close(release)

		// 시한까지 쌓인 회수 오류는 Close가 다시 보고하지 않으므로 반환값에 남아야 한다.
		if !errors.Is(err, stepErr) || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("lifecycle error = %v, want earlier close step failure and stop deadline", err)
		}
	})
}

// synctest bubble 안에서 실제 signal.Notify를 부르지 않도록 Run과 같은 옵션에 signal 구독만 바꾼다.
func runLifecycleWithoutSignals(t *testing.T, runtime *Runtime) error {
	t.Helper()

	options := runtime.lifecycleOptions()

	options.NotifySignals = func(...os.Signal) (<-chan os.Signal, func()) { return nil, func() {} }

	if err := lifecycle.Run(t.Context(), options); err != nil {
		return fmt.Errorf("run lifecycle: %w", err)
	}

	return nil
}

func runTestRuntime(group runtimeGroup, closeSteps ...func(context.Context) error) *Runtime {
	return &Runtime{
		Logger:     slog.New(slog.DiscardHandler),
		group:      group,
		closeSteps: closeSteps,
	}
}
