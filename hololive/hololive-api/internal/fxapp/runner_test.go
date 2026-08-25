package fxapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"syscall"
	"testing"
	"time"

	"go.uber.org/fx"
)

func TestRunReturnsOneAndSafetyClosesAfterStartFailure(t *testing.T) {
	application := newRunnerTestApplication()

	application.startErr = errors.New("start failed")

	code := runApplication(application, runnerTestLogger())

	if code != 1 {
		t.Fatalf("runApplication() = %d, want 1", code)
	}

	if application.safetyCloseCalls != 1 {
		t.Fatalf("SafetyClose() calls = %d, want 1", application.safetyCloseCalls)
	}
}

func TestRunReturnsZeroAfterSignalAndSuccessfulStop(t *testing.T) {
	application := newRunnerTestApplication()
	application.waitCh <- fx.ShutdownSignal{Signal: syscall.SIGTERM}

	code := runApplication(application, runnerTestLogger())

	if code != 0 {
		t.Fatalf("runApplication() = %d, want 0", code)
	}

	if application.stopCalls != 1 || application.safetyCloseCalls != 0 {
		t.Fatalf("stop calls = %d, safety close calls = %d", application.stopCalls, application.safetyCloseCalls)
	}
}

func TestRunReturnsOneForFatalShutdownSignal(t *testing.T) {
	application := newRunnerTestApplication()
	application.waitCh <- fx.ShutdownSignal{ExitCode: 1}

	if code := runApplication(application, runnerTestLogger()); code != 1 {
		t.Fatalf("runApplication() = %d, want 1", code)
	}
}

func TestRunReturnsOneForStopFailureWithoutForcedClose(t *testing.T) {
	application := newRunnerTestApplication()

	application.stopErr = errors.New("drain failed")

	application.waitCh <- fx.ShutdownSignal{Signal: syscall.SIGTERM}

	code := runApplication(application, runnerTestLogger())

	if code != 1 {
		t.Fatalf("runApplication() = %d, want 1", code)
	}

	if application.safetyCloseCalls != 0 {
		t.Fatalf("SafetyClose() calls = %d, want 0 after Stop begins", application.safetyCloseCalls)
	}
}

func TestRunReturnsOneAtHardStopDeadlineWithoutConcurrentCleanup(t *testing.T) {
	application := newRunnerTestApplication()

	application.stopTimeout = 10 * time.Millisecond
	application.stop = func(ctx context.Context) error {
		<-ctx.Done()

		return ctx.Err()
	}

	application.waitCh <- fx.ShutdownSignal{Signal: syscall.SIGTERM}

	code := runApplication(application, runnerTestLogger())

	if code != 1 {
		t.Fatalf("runApplication() = %d, want 1", code)
	}

	if application.safetyCloseCalls != 0 {
		t.Fatalf("SafetyClose() calls = %d, want 0 after stop timeout", application.safetyCloseCalls)
	}
}

type runnerTestApplication struct {
	startErr         error
	stopErr          error
	startTimeout     time.Duration
	stopTimeout      time.Duration
	waitCh           chan fx.ShutdownSignal
	stop             func(context.Context) error
	stopCalls        int
	safetyCloseCalls int
}

func newRunnerTestApplication() *runnerTestApplication {
	return &runnerTestApplication{
		startTimeout: time.Second,
		stopTimeout:  time.Second,
		waitCh:       make(chan fx.ShutdownSignal, 1),
	}
}

func (a *runnerTestApplication) Start(context.Context) error {
	return a.startErr
}

func (a *runnerTestApplication) Wait() <-chan fx.ShutdownSignal {
	return a.waitCh
}

func (a *runnerTestApplication) Stop(ctx context.Context) error {
	a.stopCalls++
	if a.stop != nil {
		if err := a.stop(ctx); err != nil {
			return fmt.Errorf("stop hook: %w", err)
		}

		return nil
	}

	return a.stopErr
}

func (a *runnerTestApplication) LifecycleTimeouts() (time.Duration, time.Duration) {
	return a.startTimeout, a.stopTimeout
}

func (a *runnerTestApplication) SafetyClose(context.Context) {
	a.safetyCloseCalls++
}

func runnerTestLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}
