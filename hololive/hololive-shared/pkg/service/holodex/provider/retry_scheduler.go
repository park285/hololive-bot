package holodexprovider

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const (
	retrySchedulerDelay   = 35 * time.Second
	retrySchedulerTimeout = 30 * time.Second
	retrySchedulerMaxSize = 10
)

type retryContextKey struct{}

type retryTask struct {
	timer *time.Timer
	key   string
}

type retryScheduler struct {
	mu      sync.Mutex
	wg      sync.WaitGroup
	pending map[string]*retryTask
	stopped bool
	maxSize int
	delay   time.Duration
	timeout time.Duration
	logger  *slog.Logger
	stopCh  chan struct{}
}

func newRetryScheduler(delay, timeout time.Duration, maxSize int, logger *slog.Logger) *retryScheduler {
	return &retryScheduler{
		pending: make(map[string]*retryTask),
		maxSize: maxSize,
		delay:   delay,
		timeout: timeout,
		logger:  logger,
		stopCh:  make(chan struct{}),
	}
}

func (s *retryScheduler) schedule(ctx context.Context, key string, fn func(ctx context.Context)) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopped {
		return
	}

	if _, exists := s.pending[key]; exists {
		return
	}

	if len(s.pending) >= s.maxSize {
		s.logger.Warn("캐시 워밍 재시도 큐 초과", slog.String("key", key), slog.Int("max_size", s.maxSize))

		return
	}

	task := &retryTask{
		key: key,
	}

	detachedCtx := context.WithoutCancel(ctx)

	task.timer = time.AfterFunc(s.delay, func() {
		s.execute(detachedCtx, task.key, fn)
	})
	s.pending[key] = task

	s.logger.Info("캐시 워밍 재시도 예약", slog.String("key", key))
}

func (s *retryScheduler) execute(parentCtx context.Context, key string, fn func(ctx context.Context)) {
	s.mu.Lock()

	if s.stopped {
		s.mu.Unlock()

		return
	}

	delete(s.pending, key)
	s.wg.Add(1)
	s.mu.Unlock()

	defer s.wg.Done()

	ctx := context.WithValue(parentCtx, retryContextKey{}, true)
	ctx, cancel := context.WithTimeout(ctx, s.timeout)

	defer cancel()

	go func() {
		select {
		case <-s.stopCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	s.logger.Info("캐시 워밍 재시도 실행", slog.String("key", key))
	fn(ctx)
}

func (s *retryScheduler) stop() {
	s.mu.Lock()

	if s.stopped {
		s.mu.Unlock()
		s.wg.Wait()

		return
	}

	s.stopped = true
	close(s.stopCh)

	for _, task := range s.pending {
		if task.timer != nil {
			task.timer.Stop()
		}
	}

	s.pending = make(map[string]*retryTask)
	s.mu.Unlock()

	s.wg.Wait()
}

func isRetryContext(ctx context.Context) bool {
	isRetry, ok := ctx.Value(retryContextKey{}).(bool)

	return ok && isRetry
}
