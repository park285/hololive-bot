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

// ComputeResult 는 ComputeFn 이 계산한 decision 과 선택적 release token.
// Token 이 nil 이면 AlreadySent, RetryLater 처럼 release 없이 확정되는 decision 을 뜻한다.
type ComputeResult struct {
	Decision Decision
	Token    *Token
}

// ComputeFn 은 cache miss 때 호출되는 ComputeResult 계산 함수.
type ComputeFn func(ctx context.Context) (ComputeResult, error)

// DecisionCache 는 batch-local get-or-compute claim decision cache.
type DecisionCache interface {
	ResolveClaim(ctx context.Context, key string, compute ComputeFn) (ResolveResult, error)
}
