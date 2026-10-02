package youtubedispatch

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

type claimDecisionCompute func(context.Context) (claimResult, error)

type claimDecisionResolved struct {
	Decision claimResult
	Hit      bool
}

// claimDecisionCache는 한 전송 배치 안에서 post별 결정을 공유합니다.
// Hit는 claim 해제 소유권과 완료 증명 재사용을 구분하며, 방별 sent 판정은 캐시 밖에서 수행합니다.
type claimDecisionCache struct {
	mu       sync.Mutex
	entries  map[string]claimResult
	inflight map[string]chan struct{}
}

func newClaimDecisionCache() *claimDecisionCache {
	return &claimDecisionCache{entries: make(map[string]claimResult), inflight: make(map[string]chan struct{})}
}

func (c *claimDecisionCache) ResolveClaim(ctx context.Context, key string, compute claimDecisionCompute) (claimDecisionResolved, error) {
	if compute == nil {
		return claimDecisionResolved{}, errors.New("resolve claim: compute is nil")
	}

	for {
		if err := ctx.Err(); err != nil {
			return claimDecisionResolved{}, fmt.Errorf("resolve claim: %w", err)
		}

		if c == nil || key == "" {
			decision, err := compute(ctx)
			if err != nil {
				return claimDecisionResolved{}, fmt.Errorf("compute claim decision: %w", err)
			}

			return claimDecisionResolved{Decision: decision}, nil
		}

		c.mu.Lock()

		if decision, ok := c.entries[key]; ok {
			c.mu.Unlock()

			return claimDecisionResolved{Decision: decision, Hit: true}, nil
		}

		if pending, ok := c.inflight[key]; ok {
			c.mu.Unlock()

			select {
			case <-pending:
				continue
			case <-ctx.Done():
				return claimDecisionResolved{}, fmt.Errorf("wait for claim decision: %w", ctx.Err())
			}
		}

		pending := make(chan struct{})

		c.inflight[key] = pending
		c.mu.Unlock()

		return c.computeAndStore(ctx, key, pending, compute)
	}
}

func (c *claimDecisionCache) computeAndStore(ctx context.Context, key string, pending chan struct{}, compute claimDecisionCompute) (claimDecisionResolved, error) {
	var decision claimResult

	stored := false
	// 오류와 panic 모두 in-flight 상태를 해제해 대기자가 영구 블록되지 않도록 합니다.
	defer func() {
		c.mu.Lock()

		if stored {
			c.entries[key] = decision
		}

		delete(c.inflight, key)
		close(pending)
		c.mu.Unlock()
	}()

	computed, err := compute(ctx)
	if err != nil {
		return claimDecisionResolved{}, fmt.Errorf("compute claim decision: %w", err)
	}

	decision, stored = computed, true

	return claimDecisionResolved{Decision: decision}, nil
}
