package cache

import (
	"context"
	"time"
)

type ConnectionManager interface {
	Close() error
	IsConnected(ctx context.Context) bool
	WaitUntilReady(ctx context.Context, timeout time.Duration) error
}
