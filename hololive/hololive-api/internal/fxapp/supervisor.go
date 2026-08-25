package fxapp

import (
	"fmt"
	"log/slog"
	"sync"

	"go.uber.org/fx"
)

type shutdownRequester interface {
	Shutdown(...fx.ShutdownOption) error
}

type supervisor struct {
	shutdowner shutdownRequester
	logger     *slog.Logger

	startOnce    sync.Once
	stopOnce     sync.Once
	terminalOnce sync.Once
	fatalOnce    sync.Once
	stopCh       chan struct{}
	doneCh       chan struct{}

	mu       sync.Mutex
	fatalErr error
}

func newSupervisor(shutdowner fx.Shutdowner, logger *slog.Logger) *supervisor {
	return newSupervisorWithShutdowner(shutdowner, logger)
}

func newSupervisorWithShutdowner(shutdowner shutdownRequester, logger *slog.Logger) *supervisor {
	return &supervisor{
		shutdowner: shutdowner,
		logger:     logger,
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
	}
}

func (s *supervisor) Start(errCh <-chan error) {
	if s == nil {
		return
	}

	s.startOnce.Do(func() {
		go s.monitor(errCh)
	})
}

func (s *supervisor) Stop() {
	if s == nil {
		return
	}

	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	<-s.doneCh
}

func (s *supervisor) Err() error {
	if s == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.fatalErr
}

func (s *supervisor) monitor(errCh <-chan error) {
	defer close(s.doneCh)

	for {
		select {
		case err := <-errCh:
			s.handleTerminal(err)
		case <-s.stopCh:
			s.drain(errCh)

			return
		}
	}
}

func (s *supervisor) drain(errCh <-chan error) {
	for {
		select {
		case err := <-errCh:
			s.handleTerminal(err)
		default:
			return
		}
	}
}

func (s *supervisor) handleTerminal(err error) {
	firstTerminal := false

	s.terminalOnce.Do(func() {
		firstTerminal = true
	})

	if err == nil {
		if firstTerminal {
			s.requestShutdown()
		}

		return
	}

	s.fatalOnce.Do(func() {
		s.mu.Lock()

		s.fatalErr = fmt.Errorf("runtime fatal: %w", err)
		s.mu.Unlock()

		logDiagnosticError(s.logger, "hololive-api runtime error", err)
		s.requestShutdown(fx.ExitCode(1))
	})
}

func (s *supervisor) requestShutdown(options ...fx.ShutdownOption) {
	if s.shutdowner == nil {
		return
	}

	if err := s.shutdowner.Shutdown(options...); err != nil {
		logDiagnosticError(s.logger, "hololive-api shutdown request failed", err)
	}
}
