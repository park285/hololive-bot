package holodex

import (
	"context"
	"log/slog"
	"sync"
	"time"
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
}

func newRetryScheduler(delay, timeout time.Duration, maxSize int, logger *slog.Logger) *retryScheduler {
	return &retryScheduler{
		pending: make(map[string]*retryTask),
		maxSize: maxSize,
		delay:   delay,
		timeout: timeout,
		logger:  logger,
	}
}

func (s *retryScheduler) schedule(key string, fn func(ctx context.Context)) {
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
	task.timer = time.AfterFunc(s.delay, func() {
		s.execute(task.key, fn)
	})
	s.pending[key] = task

	s.logger.Info("캐시 워밍 재시도 예약", slog.String("key", key))
}

func (s *retryScheduler) execute(key string, fn func(ctx context.Context)) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	delete(s.pending, key)
	s.wg.Add(1)
	s.mu.Unlock()

	defer s.wg.Done()

	ctx := context.WithValue(context.Background(), retryContextKey{}, true)
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

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
	for _, task := range s.pending {
		if task.timer != nil {
			task.timer.Stop()
		}
	}
	s.pending = make(map[string]*retryTask)
	s.mu.Unlock()

	s.wg.Wait()
}

func (s *retryScheduler) pendingCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pending)
}

func isRetryContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}

	isRetry, ok := ctx.Value(retryContextKey{}).(bool)
	return ok && isRetry
}
