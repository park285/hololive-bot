// Package cleanupctx creates bounded cleanup contexts that preserve values while
// detaching cleanup work from an already-canceled request or runtime context.
package cleanupctx

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// DefaultTimeout is the fallback deadline for detached cleanup work.
const DefaultTimeout = 5 * time.Second

// ErrNilDone reports that Wait received no completion channel.
var ErrNilDone = errors.New("cleanup done channel is nil")

// WithTimeout returns a context for cleanup work. Values from parent are
// preserved, while cancellation and deadlines from parent are deliberately
// detached. A non-positive timeout uses DefaultTimeout.
func WithTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	} else {
		parent = context.WithoutCancel(parent)
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return context.WithTimeout(parent, timeout)
}

// Wait waits for done using a detached, bounded cleanup context.
func Wait(parent context.Context, timeout time.Duration, done <-chan struct{}) error {
	if done == nil {
		return ErrNilDone
	}
	ctx, cancel := WithTimeout(parent, timeout)
	defer cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for cleanup: %w", ctx.Err())
	}
}
