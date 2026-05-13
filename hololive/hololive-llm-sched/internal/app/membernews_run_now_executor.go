package app

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

type memberNewsRunNowExecutor struct {
	baseCtx context.Context
	timeout time.Duration
	sender  memberNewsWeeklyDigestSender
	logger  *slog.Logger
	stateMu sync.Mutex
	running bool
	pending bool
}

func newMemberNewsRunNowExecutor(
	baseCtx context.Context,
	sender memberNewsWeeklyDigestSender,
	timeout time.Duration,
	logger *slog.Logger,
) *memberNewsRunNowExecutor {
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &memberNewsRunNowExecutor{
		baseCtx: baseCtx,
		timeout: timeout,
		sender:  sender,
		logger:  logger,
	}
}

func (e *memberNewsRunNowExecutor) Trigger() {
	e.stateMu.Lock()
	if e.running {
		e.pending = true
		e.stateMu.Unlock()
		e.logger.Info("Member news weekly run-now coalesced: run already in progress")
		return
	}
	e.running = true
	e.stateMu.Unlock()

	go e.runLoop()
}

func (e *memberNewsRunNowExecutor) runLoop() {
	for {
		e.runOnce()

		e.stateMu.Lock()
		if !e.pending {
			e.running = false
			e.stateMu.Unlock()
			return
		}
		e.pending = false
		e.stateMu.Unlock()

		e.logger.Info("Member news weekly run-now processing coalesced trigger")
	}
}

func (e *memberNewsRunNowExecutor) runOnce() {
	if e.sender == nil {
		e.logger.Warn("Ignored membernews weekly run-now: scheduler not initialized")
		return
	}

	runCtx, cancel := context.WithTimeout(e.baseCtx, e.timeout)
	defer cancel()

	if err := e.sender.SendWeeklyDigest(runCtx); err != nil {
		e.logger.Error("Member news weekly run-now failed", slog.Any("error", err))
		return
	}

	e.logger.Info("Member news weekly run-now completed via config update")
}
