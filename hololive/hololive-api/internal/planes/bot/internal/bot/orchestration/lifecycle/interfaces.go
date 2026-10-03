package lifecycle

import (
	"context"
	"time"
)

type IrisPinger interface {
	Ping(ctx context.Context) bool
}

type CacheReadiness interface {
	WaitUntilReady(context.Context, time.Duration) error
}
