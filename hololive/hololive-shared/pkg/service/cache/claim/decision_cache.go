package claim

import (
	"context"
	"time"
)

// Decision 은 get-or-compute claim cache 에 저장되는 호출부 결정값.
type Decision struct {
	AuthorizedAt time.Time
	Value        any
}

// Token 은 miss 에서 새로 계산해 저장한 decision 의 release / mark-sent 핸들.
type Token struct {
	AuthorizedAt time.Time
}

// ResolveResult 는 ResolveClaim 의 hit/miss 결과.
type ResolveResult struct {
	Decision Decision
	Token    *Token
	Hit      bool
}

// ComputeFn 은 cache miss 때 호출되는 decision 계산 함수.
type ComputeFn func(ctx context.Context) (Decision, error)

// DecisionCache 는 batch-local get-or-compute claim decision cache.
type DecisionCache interface {
	ResolveClaim(ctx context.Context, key string, compute ComputeFn) (ResolveResult, error)
}
