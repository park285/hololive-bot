package holodexprovider

import (
	"context"
	"fmt"
	"sync"
)

// 완료 신호만 공유하며 결과와 오류는 각 호출자의 기존 캐시/조회 경로에 맡긴다.
type streamCacheFillGate struct {
	mu     sync.Mutex
	owners map[string]chan struct{}
}

func (g *streamCacheFillGate) acquire(ctx context.Context, key string) error {
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("wait for stream cache fill: %w", err)
		}

		pending, occupied := g.pendingOrClaim(key)

		if !occupied {
			if err := ctx.Err(); err != nil {
				g.release(key)

				return fmt.Errorf("acquire stream cache fill: %w", err)
			}

			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for stream cache fill: %w", ctx.Err())
		case <-pending:
		}
	}
}

func (g *streamCacheFillGate) pendingOrClaim(key string) (chan struct{}, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if pending, occupied := g.owners[key]; occupied {
		return pending, true
	}

	if g.owners == nil {
		g.owners = make(map[string]chan struct{})
	}

	g.owners[key] = make(chan struct{})

	return nil, false
}

func (g *streamCacheFillGate) release(key string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	pending := g.owners[key]
	delete(g.owners, key)
	close(pending)
}
