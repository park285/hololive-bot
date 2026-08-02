package claim

import (
	"context"
	"sync"
)

// MemoryDecisionCache 는 DecisionCache 의 in-memory 구현.
type MemoryDecisionCache struct {
	mu       sync.Mutex
	entries  map[string]Decision
	inflight map[string]chan struct{}
}

// NewMemoryDecisionCache 는 key 단위 single-flight DecisionCache 를 반환.
func NewMemoryDecisionCache() *MemoryDecisionCache {
	return &MemoryDecisionCache{
		entries:  make(map[string]Decision),
		inflight: make(map[string]chan struct{}),
	}
}

func (c *MemoryDecisionCache) ResolveClaim(ctx context.Context, key string, compute ComputeFn) (ResolveResult, error) {
	if compute == nil {
		return ResolveResult{}, ErrNilCompute
	}
	if err := ctx.Err(); err != nil {
		return ResolveResult{}, err
	}
	if key == "" || c == nil {
		return computeDecision(ctx, compute)
	}

	return c.resolveKeyedClaim(ctx, key, compute)
}

func (c *MemoryDecisionCache) resolveKeyedClaim(ctx context.Context, key string, compute ComputeFn) (ResolveResult, error) {
	for {
		hit, pending, owner := c.lookupOrClaim(key)
		if owner {
			return c.computeAndStore(ctx, key, compute)
		}
		if pending == nil {
			return hit, nil
		}
		if err := awaitDecision(ctx, pending); err != nil {
			return ResolveResult{}, err
		}
	}
}

func (c *MemoryDecisionCache) lookupOrClaim(key string) (hit ResolveResult, pending <-chan struct{}, owner bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if decision, ok := c.entries[key]; ok {
		return ResolveResult{Decision: decision, Hit: true}, nil, false
	}
	if inflight, ok := c.inflight[key]; ok {
		return ResolveResult{}, inflight, false
	}

	c.inflight[key] = make(chan struct{})
	return ResolveResult{}, nil, true
}

func (c *MemoryDecisionCache) computeAndStore(ctx context.Context, key string, compute ComputeFn) (ResolveResult, error) {
	var (
		result ResolveResult
		stored bool
	)
	// compute 가 panic 해도 inflight 를 반드시 비워야 대기자가 영구 블록되지 않는다.
	defer func() { c.releaseKey(key, result.Decision, stored) }()

	result, err := computeDecision(ctx, compute)
	if err != nil {
		return ResolveResult{}, err
	}
	stored = true

	return result, nil
}

func (c *MemoryDecisionCache) releaseKey(key string, decision Decision, store bool) {
	c.mu.Lock()
	inflight := c.inflight[key]
	delete(c.inflight, key)
	if store {
		c.entries[key] = decision
	}
	c.mu.Unlock()

	if inflight != nil {
		close(inflight)
	}
}

func awaitDecision(ctx context.Context, pending <-chan struct{}) error {
	select {
	case <-pending:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func computeDecision(ctx context.Context, compute ComputeFn) (ResolveResult, error) {
	decision, token, err := compute(ctx)
	if err != nil {
		return ResolveResult{}, err
	}
	return ResolveResult{
		Decision: decision,
		Token:    token,
	}, nil
}
