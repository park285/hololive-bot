package fallback

import (
	"context"
	"sync"

	"golang.org/x/sync/errgroup"
)

type Trigger string

const (
	TriggerOnFailures              Trigger = "on_failures"
	TriggerOnEmptyPrimary          Trigger = "on_empty_primary"
	TriggerOnEmptyPrimaryWithError Trigger = "on_empty_primary_with_error"
)

// Policy: 후속 fallback 실행 조건 초안.
type Policy struct {
	Trigger Trigger
}

func (p Policy) ShouldRun(primaryResults int, failedTargets int) bool {
	switch p.Trigger {
	case TriggerOnEmptyPrimary:
		return primaryResults == 0
	case TriggerOnEmptyPrimaryWithError:
		return primaryResults == 0 && failedTargets > 0
	case TriggerOnFailures, "":
		return failedTargets > 0
	default:
		return false
	}
}

// FetchPlan: primary fan-out 실행 계획 초안.
// 현재는 제한 병렬성과 성공 callback만 공통화하고, 호출자는 후속 fallback 실행을 직접 담당한다.
// OnSuccess는 Parallelism > 1일 때 동시 호출될 수 있으므로, 호출자 측에서 필요한 동기화를 해야 한다.
type FetchPlan[K any, V any] struct {
	Targets     []K
	Parallelism int
	Fetch       func(context.Context, K) (V, error)
	OnSuccess   func(K, V)
}

// Summary: primary phase fan-out 실행 결과.
type Summary[K any] struct {
	SuccessCount  int
	FailedCount   int
	FailedTargets []K
}

func (s Summary[K]) HasFailures() bool {
	return s.FailedCount > 0
}

func (s Summary[K]) AllFailed(totalTargets int) bool {
	return totalTargets > 0 && s.SuccessCount == 0 && s.FailedCount == totalTargets
}

// Execute: key fan-out primary fetch를 실행하고 실패 key를 원래 순서대로 수집한다.
// 개별 key 실패는 전체 실행을 중단하지 않고 후속 fallback 후보로 남긴다.
func Execute[K any, V any](
	ctx context.Context,
	plan FetchPlan[K, V],
) Summary[K] {
	summary := Summary[K]{
		FailedTargets: make([]K, 0, len(plan.Targets)),
	}
	if len(plan.Targets) == 0 {
		return summary
	}

	failed := make([]bool, len(plan.Targets))
	var mu sync.Mutex

	parallelism := plan.Parallelism
	if parallelism <= 1 {
		for i := range plan.Targets {
			value, err := plan.Fetch(ctx, plan.Targets[i])
			if err != nil {
				failed[i] = true
				continue
			}
			summary.SuccessCount++
			if plan.OnSuccess != nil {
				plan.OnSuccess(plan.Targets[i], value)
			}
		}
	} else {
		eg, egCtx := errgroup.WithContext(ctx)
		eg.SetLimit(parallelism)

		for i := range plan.Targets {
			i := i
			key := plan.Targets[i]
			eg.Go(func() error {
				value, err := plan.Fetch(egCtx, key)
				if err != nil {
					mu.Lock()
					failed[i] = true
					mu.Unlock()
					return nil
				}

				if plan.OnSuccess != nil {
					plan.OnSuccess(key, value)
				}
				mu.Lock()
				summary.SuccessCount++
				mu.Unlock()
				return nil
			})
		}
		_ = eg.Wait()
	}

	for i := range plan.Targets {
		if failed[i] {
			summary.FailedCount++
			summary.FailedTargets = append(summary.FailedTargets, plan.Targets[i])
		}
	}

	return summary
}

// PrimaryResult: 기존 call site 호환용 실행 결과.
type PrimaryResult[K any] struct {
	Attempted int
	Succeeded int
	Failed    []K
}

func (r PrimaryResult[K]) HasFailures() bool {
	return len(r.Failed) > 0
}

func (r PrimaryResult[K]) AllFailed() bool {
	return r.Attempted > 0 && r.Succeeded == 0 && len(r.Failed) == r.Attempted
}

// RunPrimary: 기존 call site용 thin wrapper.
func RunPrimary[K any](
	ctx context.Context,
	keys []K,
	plan FetchPlan[K, struct{}],
	run func(context.Context, K) error,
) PrimaryResult[K] {
	summary := Execute(ctx, FetchPlan[K, struct{}]{
		Targets:     keys,
		Parallelism: plan.Parallelism,
		Fetch: func(fetchCtx context.Context, key K) (struct{}, error) {
			return struct{}{}, run(fetchCtx, key)
		},
	})

	return PrimaryResult[K]{
		Attempted: len(keys),
		Succeeded: summary.SuccessCount,
		Failed:    summary.FailedTargets,
	}
}
