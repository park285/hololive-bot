package claim

import (
	"context"
	"errors"
	"time"
)

// ClaimKey 는 reuse cache 의 claim 식별자.
// Scope 는 도메인 (예: "youtube_outbox_delivery"), Subject 는 도메인 내 식별자.
type ClaimKey struct {
	Scope   string
	Subject string
}

// ClaimStatus 는 reuse cache 에서 claim 의 현재 상태.
// RetryAfter 는 외부에 노출되는 cool-down hint (claim 실패 시 비어 있지 않음).
type ClaimStatus struct {
	Holder     string
	AcquiredAt time.Time
	ExpiresAt  time.Time
	RetryAfter time.Duration
}

// ReuseCache 는 claim 토큰 + cool-down 기반 reuse cache 의 추상화.
type ReuseCache interface {
	// Claim 은 key 에 대한 claim 을 시도. 성공 시 ClaimStatus, 실패 시 RetryAfter 가 채워진 ClaimStatus 와 ErrAlreadyHeld.
	Claim(ctx context.Context, key ClaimKey, holder string, ttl time.Duration) (ClaimStatus, error)
	// Release 는 key 의 claim 을 해제. holder mismatch 시 ErrHolderMismatch.
	Release(ctx context.Context, key ClaimKey, holder string) error
}

var (
	ErrAlreadyHeld    = errors.New("claim: key already held by another holder")
	ErrHolderMismatch = errors.New("claim: holder mismatch on release")
	ErrEmptyKey       = errors.New("claim: empty key")
	ErrEmptyHolder    = errors.New("claim: empty holder")
	ErrInvalidTTL     = errors.New("claim: ttl must be positive")
)
