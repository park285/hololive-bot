// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package fallback

import (
	"context"
	"fmt"
	"sync"

	"github.com/park285/shared-go/v2/pkg/panicguard"
)

type Trigger string

const (
	TriggerOnFailures              Trigger = "on_failures"
	TriggerOnEmptyPrimary          Trigger = "on_empty_primary"
	TriggerOnEmptyPrimaryWithError Trigger = "on_empty_primary_with_error"
)

type Policy struct {
	Trigger Trigger
}

func (p Policy) ShouldRun(primaryResults, failedTargets int) bool {
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

// 현재는 제한 병렬성과 성공 callback만 공통화하고, 호출자는 후속 fallback 실행을 직접 담당한다.
// OnSuccess는 Parallelism > 1일 때 동시 호출될 수 있으므로, 호출자 측에서 필요한 동기화를 해야 한다.
type FetchPlan[K, V any] struct {
	Targets     []K
	Parallelism int
	Fetch       func(context.Context, K) (V, error)
	OnSuccess   func(K, V)
}

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

// 개별 key 실패는 전체 실행을 중단하지 않고 후속 fallback 후보로 남긴다.
func (plan FetchPlan[K, V]) Execute(ctx context.Context) Summary[K] {
	if len(plan.Targets) == 0 {
		return Summary[K]{FailedTargets: []K{}}
	}

	failed := make([]bool, len(plan.Targets))

	var successCount int

	if plan.Parallelism <= 1 {
		successCount = plan.executeSequential(ctx, failed)
	} else {
		successCount = plan.executeParallel(ctx, failed)
	}

	return summarizeFailures(plan.Targets, failed, successCount)
}

func (plan FetchPlan[K, V]) executeSequential(ctx context.Context, failed []bool) int {
	successCount := 0

	for i := range plan.Targets {
		value, err := plan.Fetch(ctx, plan.Targets[i])
		if err != nil {
			failed[i] = true
			continue
		}

		successCount++

		if plan.OnSuccess != nil {
			plan.OnSuccess(plan.Targets[i], value)
		}
	}

	return successCount
}

type parallelResult struct {
	mu           sync.Mutex
	failed       []bool
	successCount int
}

func (r *parallelResult) markFailed(index int) {
	r.mu.Lock()

	r.failed[index] = true
	r.mu.Unlock()
}

func (r *parallelResult) markSuccess() {
	r.mu.Lock()

	r.successCount++
	r.mu.Unlock()
}

func (plan FetchPlan[K, V]) fetchParallelTarget(ctx context.Context, key K, result *parallelResult) error {
	value, err := plan.Fetch(ctx, key)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}

	if plan.OnSuccess != nil {
		plan.OnSuccess(key, value)
	}

	result.markSuccess()

	return nil
}

// 개별 key 실패를 goroutine 안에서 흡수한다. 만약 errgroup처럼 첫 실패로 ctx를 취소하면
// 아직 실행 중인 다른 target까지 중단되어 fallback 후보 집계가 어긋난다.
func (plan FetchPlan[K, V]) executeParallel(ctx context.Context, failed []bool) int {
	limiter := make(chan struct{}, plan.Parallelism)
	result := parallelResult{failed: failed}

	var wg sync.WaitGroup

	for i := range plan.Targets {
		key := plan.Targets[i]

		limiter <- struct{}{}

		wg.Go(func() {
			defer func() { <-limiter }()

			if err := panicguard.RunE(nil, panicguard.BackgroundTask, "fallback-fetch", func() error {
				return plan.fetchParallelTarget(ctx, key, &result)
			}); err != nil {
				result.markFailed(i)
			}
		})
	}

	wg.Wait()

	return result.successCount
}

func summarizeFailures[K any](targets []K, failed []bool, successCount int) Summary[K] {
	summary := Summary[K]{
		SuccessCount:  successCount,
		FailedTargets: make([]K, 0, len(targets)),
	}
	for i := range targets {
		if !failed[i] {
			continue
		}

		summary.FailedCount++

		summary.FailedTargets = append(summary.FailedTargets, targets[i])
	}

	return summary
}

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

func (plan FetchPlan[K, _]) RunPrimary(
	ctx context.Context,
	keys []K,
	run func(context.Context, K) error,
) PrimaryResult[K] {
	summary := FetchPlan[K, struct{}]{
		Targets:     keys,
		Parallelism: plan.Parallelism,
		Fetch: func(fetchCtx context.Context, key K) (struct{}, error) {
			return struct{}{}, run(fetchCtx, key)
		},
	}.Execute(ctx)

	return PrimaryResult[K]{
		Attempted: len(keys),
		Succeeded: summary.SuccessCount,
		Failed:    summary.FailedTargets,
	}
}
