package fxapp

import (
	"errors"
	"log/slog"
	"testing"
	"time"

	"go.uber.org/fx"
)

func TestSupervisorFatalRequestsFailureShutdown(t *testing.T) {
	shutdowner := newSupervisorTestShutdowner()
	supervisor := newSupervisorWithShutdowner(shutdowner, supervisorTestLogger())
	errCh := make(chan error, 1)
	fatalErr := errors.New("listener failed")

	supervisor.Start(errCh)

	errCh <- fatalErr

	call := receiveShutdownCall(t, shutdowner.calls)
	if call.optionCount != 1 {
		t.Fatalf("Shutdown() option count = %d, want failure exit option", call.optionCount)
	}

	supervisor.Stop()

	if !errors.Is(supervisor.Err(), fatalErr) {
		t.Fatalf("Err() = %v, want fatal error", supervisor.Err())
	}
}

func TestSupervisorNilTerminalRequestsCleanShutdown(t *testing.T) {
	shutdowner := newSupervisorTestShutdowner()
	supervisor := newSupervisorWithShutdowner(shutdowner, supervisorTestLogger())
	errCh := make(chan error, 1)
	supervisor.Start(errCh)

	errCh <- nil

	call := receiveShutdownCall(t, shutdowner.calls)
	if call.optionCount != 0 {
		t.Fatalf("Shutdown() option count = %d, want clean shutdown", call.optionCount)
	}

	supervisor.Stop()

	if supervisor.Err() != nil {
		t.Fatalf("Err() = %v, want nil", supervisor.Err())
	}
}

func TestSupervisorFatalAfterCleanTerminalStillWins(t *testing.T) {
	shutdowner := newSupervisorTestShutdowner()
	supervisor := newSupervisorWithShutdowner(shutdowner, supervisorTestLogger())
	errCh := make(chan error, 1)
	fatalErr := errors.New("fatal after clean terminal")

	supervisor.Start(errCh)

	errCh <- nil

	receiveShutdownCall(t, shutdowner.calls)

	errCh <- fatalErr

	call := receiveShutdownCall(t, shutdowner.calls)
	if call.optionCount != 1 {
		t.Fatalf("second Shutdown() option count = %d, want failure exit option", call.optionCount)
	}

	supervisor.Stop()

	if !errors.Is(supervisor.Err(), fatalErr) {
		t.Fatalf("Err() = %v, want fatal error", supervisor.Err())
	}
}

func TestSupervisorPreservesFirstFatalAndDrainsLaterReports(t *testing.T) {
	shutdowner := newSupervisorTestShutdowner()
	supervisor := newSupervisorWithShutdowner(shutdowner, supervisorTestLogger())
	errCh := make(chan error, 2)
	firstErr := errors.New("first fatal")
	secondErr := errors.New("second fatal")

	supervisor.Start(errCh)

	errCh <- firstErr

	receiveShutdownCall(t, shutdowner.calls)

	errCh <- secondErr

	supervisor.Stop()

	if !errors.Is(supervisor.Err(), firstErr) {
		t.Fatalf("Err() = %v, want first fatal", supervisor.Err())
	}

	if errors.Is(supervisor.Err(), secondErr) {
		t.Fatalf("Err() = %v, want only first fatal", supervisor.Err())
	}
}

type supervisorTestShutdownCall struct {
	optionCount int
}

type supervisorTestShutdowner struct {
	calls chan supervisorTestShutdownCall
}

func newSupervisorTestShutdowner() *supervisorTestShutdowner {
	return &supervisorTestShutdowner{calls: make(chan supervisorTestShutdownCall, 4)}
}

func (s *supervisorTestShutdowner) Shutdown(options ...fx.ShutdownOption) error {
	s.calls <- supervisorTestShutdownCall{optionCount: len(options)}

	return nil
}

func receiveShutdownCall(t *testing.T, calls <-chan supervisorTestShutdownCall) supervisorTestShutdownCall {
	t.Helper()

	select {
	case call := <-calls:
		return call
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for shutdown request")

		return supervisorTestShutdownCall{}
	}
}

func supervisorTestLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}
