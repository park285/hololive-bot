package lifecycle

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type SignalNotifier func(signals ...os.Signal) (<-chan os.Signal, func())

type Options struct {
	BaseContext     context.Context
	ShutdownTimeout time.Duration
	Signals         []os.Signal
	NotifySignals   SignalNotifier
	Start           func(ctx context.Context, errCh chan<- error)
	OnSignal        func(os.Signal)
	OnError         func(error)
	BeforeShutdown  func()
	Shutdown        func(ctx context.Context) error
}

func Run(opts Options) error {
	baseCtx := opts.BaseContext
	if baseCtx == nil {
		baseCtx = context.Background()
	}

	notifySignals := opts.NotifySignals
	if notifySignals == nil {
		notifySignals = defaultSignalNotifier
	}

	signals := opts.Signals
	if len(signals) == 0 {
		signals = []os.Signal{syscall.SIGINT, syscall.SIGTERM}
	}

	sigCh, stopSignals := notifySignals(signals...)
	if stopSignals == nil {
		stopSignals = func() {}
	}
	defer stopSignals()

	runCtx, cancel := context.WithCancel(baseCtx)
	defer cancel()

	errCh := make(chan error, 1)
	if opts.Start != nil {
		opts.Start(runCtx, errCh)
	}

	select {
	case sig := <-sigCh:
		if opts.OnSignal != nil {
			opts.OnSignal(sig)
		}
	case err := <-errCh:
		if opts.OnError != nil {
			opts.OnError(err)
		}
	case <-baseCtx.Done():
	}

	if opts.BeforeShutdown != nil {
		opts.BeforeShutdown()
	}

	cancel()

	shutdownCtx := context.Background()
	shutdownCancel := func() {}
	if opts.ShutdownTimeout > 0 {
		shutdownCtx, shutdownCancel = context.WithTimeout(context.Background(), opts.ShutdownTimeout)
	}
	defer shutdownCancel()

	if opts.Shutdown == nil {
		return nil
	}

	return opts.Shutdown(shutdownCtx)
}

func defaultSignalNotifier(signals ...os.Signal) (<-chan os.Signal, func()) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, signals...)
	return sigCh, func() {
		signal.Stop(sigCh)
	}
}
