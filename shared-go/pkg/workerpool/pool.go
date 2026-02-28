package workerpool

import (
	"context"
	"sync"
	"time"

	"github.com/panjf2000/ants/v2"
)

type Pool struct {
	pool *ants.Pool
	wg   sync.WaitGroup
}

type Config struct {
	Size            int
	PreAlloc        bool
	ExpiryDuration  time.Duration
	Nonblocking     bool
	MaxBlockingTask int
}

func DefaultConfig() Config {
	return Config{
		Size:            10,
		PreAlloc:        false,
		ExpiryDuration:  time.Second * 10,
		Nonblocking:     false,
		MaxBlockingTask: 0,
	}
}

func New(cfg Config) (*Pool, error) {
	opts := []ants.Option{
		ants.WithExpiryDuration(cfg.ExpiryDuration),
		ants.WithNonblocking(cfg.Nonblocking),
	}
	if cfg.PreAlloc {
		opts = append(opts, ants.WithPreAlloc(true))
	}
	if cfg.MaxBlockingTask > 0 {
		opts = append(opts, ants.WithMaxBlockingTasks(cfg.MaxBlockingTask))
	}

	pool, err := ants.NewPool(cfg.Size, opts...)
	if err != nil {
		return nil, err
	}
	return &Pool{pool: pool}, nil
}

func (p *Pool) Submit(task func()) error {
	p.wg.Add(1)
	err := p.pool.Submit(func() {
		defer p.wg.Done()
		task()
	})
	if err != nil {
		p.wg.Done()
		return err
	}
	return nil
}

func (p *Pool) Running() int {
	return p.pool.Running()
}

func (p *Pool) Cap() int {
	return p.pool.Cap()
}

func (p *Pool) Free() int {
	return p.pool.Free()
}

func (p *Pool) Wait() {
	p.wg.Wait()
}

func (p *Pool) Shutdown() {
	p.pool.Release()
}

// ShutdownWait gracefully shuts down the worker pool with context-aware timeout.
//
// Behavior:
//   - Normal case: Waits for all in-flight tasks to complete, then releases the pool
//   - Context canceled: Attempts graceful shutdown with a grace period, then releases the pool
//
// Goroutine Lifecycle:
//
//	This function spawns an internal goroutine to monitor task completion (via WaitGroup).
//	- If all tasks complete before context cancellation, the goroutine exits naturally
//	- If context is canceled first, the goroutine remains active until remaining tasks finish
//	- This is intentional: the goroutine ensures proper WaitGroup cleanup and is bounded
//	  (it will complete when the last task finishes, which is guaranteed to happen eventually)
//
// Grace Period on Cancellation:
//
//	When context is canceled, the function uses ReleaseTimeout() to give workers a grace period:
//	- If ctx has a deadline, grace period = time until deadline
//	- Otherwise, grace period = 5 seconds (default)
//	- This allows tasks to complete cleanly before forceful termination
//
// Return Values:
//   - nil: All tasks completed successfully and pool is released
//   - ctx.Err(): Context was canceled (pool is released but some tasks may still be finishing)
func (p *Pool) ShutdownWait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All tasks completed naturally
		p.pool.Release()
		return nil
	case <-ctx.Done():
		// Context canceled - determine grace period from context deadline
		gracePeriod := 5 * time.Second
		if deadline, ok := ctx.Deadline(); ok {
			remaining := time.Until(deadline)
			gracePeriod = max(remaining,
				// Deadline already passed
				0)
		}

		// ReleaseTimeout waits for workers to exit before timing out
		// This gives in-flight tasks a chance to complete cleanly
		_ = p.pool.ReleaseTimeout(gracePeriod) //nolint:errcheck
		return ctx.Err()
	}
}
