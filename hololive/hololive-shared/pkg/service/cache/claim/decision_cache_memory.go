package claim

import (
	"context"
	"sync"
)

// MemoryDecisionCache 는 DecisionCache 의 in-memory 구현.
type MemoryDecisionCache struct {
	mu      sync.Mutex
	entries map[string]Decision
}

// NewMemoryDecisionCache 는 sync.Mutex 기반 DecisionCache 를 반환.
func NewMemoryDecisionCache() *MemoryDecisionCache {
	return &MemoryDecisionCache{
		entries: make(map[string]Decision),
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

	c.mu.Lock()
	defer c.mu.Unlock()

	if decision, ok := c.entries[key]; ok {
		return ResolveResult{
			Decision: decision,
			Hit:      true,
		}, nil
	}

	result, err := computeDecision(ctx, compute)
	if err != nil {
		return ResolveResult{}, err
	}
	c.entries[key] = result.Decision
	return result, nil
}

func computeDecision(ctx context.Context, compute ComputeFn) (ResolveResult, error) {
	decision, err := compute(ctx)
	if err != nil {
		return ResolveResult{}, err
	}
	return ResolveResult{
		Decision: decision,
		Token: &Token{
			AuthorizedAt: decision.AuthorizedAt,
		},
	}, nil
}
