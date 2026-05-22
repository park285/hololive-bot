package claim

import (
	"context"
	"strings"
	"sync"
	"time"
)

// MemoryCache 는 ReuseCache 의 in-memory 구현 (테스트 + local 용).
// 외부 cache (Valkey/Redis) 가 없는 환경에서 fallback 으로 사용 가능.
type MemoryCache struct {
	mu       sync.Mutex
	holdings map[ClaimKey]ClaimStatus
	now      func() time.Time
}

// NewMemoryCache 는 sync.Mutex 기반 ReuseCache 를 반환.
func NewMemoryCache() *MemoryCache {
	return &MemoryCache{
		holdings: make(map[ClaimKey]ClaimStatus),
		now:      time.Now,
	}
}

func (c *MemoryCache) Claim(ctx context.Context, key ClaimKey, holder string, ttl time.Duration) (ClaimStatus, error) {
	if err := validateClaim(key, holder, ttl); err != nil {
		return ClaimStatus{}, err
	}
	if err := ctx.Err(); err != nil {
		return ClaimStatus{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	if existing, ok := c.holdings[key]; ok && existing.ExpiresAt.After(now) {
		return ClaimStatus{
			Holder:     existing.Holder,
			AcquiredAt: existing.AcquiredAt,
			ExpiresAt:  existing.ExpiresAt,
			RetryAfter: existing.ExpiresAt.Sub(now),
		}, ErrAlreadyHeld
	}

	status := ClaimStatus{
		Holder:     holder,
		AcquiredAt: now,
		ExpiresAt:  now.Add(ttl),
	}
	c.holdings[key] = status
	return status, nil
}

func (c *MemoryCache) Release(ctx context.Context, key ClaimKey, holder string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	existing, ok := c.holdings[key]
	if !ok {
		return nil
	}
	if existing.Holder != holder {
		return ErrHolderMismatch
	}
	delete(c.holdings, key)
	return nil
}

func validateClaim(key ClaimKey, holder string, ttl time.Duration) error {
	if strings.TrimSpace(key.Scope) == "" || strings.TrimSpace(key.Subject) == "" {
		return ErrEmptyKey
	}
	if strings.TrimSpace(holder) == "" {
		return ErrEmptyHolder
	}
	if ttl <= 0 {
		return ErrInvalidTTL
	}
	return nil
}
