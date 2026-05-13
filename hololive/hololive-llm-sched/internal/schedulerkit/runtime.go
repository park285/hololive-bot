package schedulerkit

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

type StopReason int

const (
	StopReasonContextCancelled StopReason = iota + 1
	StopReasonManual
)

type Config struct {
	Logger           *slog.Logger
	WaitingLog       string
	ContextStopLog   string
	StopLog          string
	CalculateNextRun func(time.Time) time.Time
	OnTick           func(context.Context)
	OnStop           func(StopReason)
}

type Runtime struct {
	initOnce sync.Once
	mu       sync.Mutex

	now      func() time.Time
	stopCh   chan struct{}
	stopOnce *sync.Once
	started  bool

	wg sync.WaitGroup
}

func NewRuntime() *Runtime {
	rt := &Runtime{}
	rt.init()
	return rt
}

func (r *Runtime) SetClock(clockFn func() time.Time) {
	if r == nil || clockFn == nil {
		return
	}
	r.init()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.now = clockFn
}

func (r *Runtime) Now() time.Time {
	if r == nil {
		return time.Now()
	}
	r.init()
	r.mu.Lock()
	now := r.now
	r.mu.Unlock()
	return now()
}

func (r *Runtime) Start(ctx context.Context, cfg Config) {
	if r == nil || cfg.CalculateNextRun == nil || cfg.OnTick == nil {
		return
	}
	r.init()

	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return
	}
	stopCh := r.stopCh
	r.started = true
	r.wg.Add(1)
	r.mu.Unlock()

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	go func(stopCh chan struct{}) {
		defer r.wg.Done()

		reason := r.run(ctx, cfg, logger, stopCh)
		r.finishRun(stopCh)
		if cfg.OnStop != nil {
			cfg.OnStop(reason)
		}
	}(stopCh)
}

func (r *Runtime) Stop() {
	if r == nil {
		return
	}
	r.init()
	r.mu.Lock()
	stopOnce := r.stopOnce
	stopCh := r.stopCh
	started := r.started
	r.mu.Unlock()

	if started {
		stopOnce.Do(func() {
			close(stopCh)
		})
	}

	r.wg.Wait()
}

func (r *Runtime) run(ctx context.Context, cfg Config, logger *slog.Logger, stopCh <-chan struct{}) StopReason {
	for {
		nextRun := cfg.CalculateNextRun(r.Now())
		logger.Info(cfg.WaitingLog,
			slog.Time("next_run", nextRun),
			slog.Duration("wait_duration", time.Until(nextRun)),
		)

		timer := time.NewTimer(time.Until(nextRun))
		if reason, stopped := waitRuntimeTick(ctx, cfg, logger, stopCh, timer); stopped {
			return reason
		}
		cfg.OnTick(ctx)
	}
}

func waitRuntimeTick(
	ctx context.Context,
	cfg Config,
	logger *slog.Logger,
	stopCh <-chan struct{},
	timer *time.Timer,
) (StopReason, bool) {
	select {
	case <-ctx.Done():
		stopTimer(timer)
		logger.Info(cfg.ContextStopLog)
		return StopReasonContextCancelled, true
	case <-stopCh:
		stopTimer(timer)
		logger.Info(cfg.StopLog)
		return StopReasonManual, true
	case <-timer.C:
		return 0, false
	}
}

func (r *Runtime) finishRun(stopCh chan struct{}) {
	r.init()

	r.mu.Lock()
	currentStopCh := r.stopCh
	stopOnce := r.stopOnce
	r.mu.Unlock()

	if currentStopCh == stopCh {
		stopOnce.Do(func() {
			close(stopCh)
		})
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.started = false
	if r.stopCh == stopCh {
		r.stopCh = make(chan struct{})
		r.stopOnce = &sync.Once{}
	}
}

func (r *Runtime) init() {
	r.initOnce.Do(func() {
		r.now = time.Now
		r.stopCh = make(chan struct{})
		r.stopOnce = &sync.Once{}
	})
}

func stopTimer(timer *time.Timer) {
	if timer == nil {
		return
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}
