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

// FetchPlan은 primary 단계의 target 조회를 제한 병렬로 실행한다. 후속 fallback 실행 여부는 호출자가 결과를 보고 정한다.
type FetchPlan[K any] struct {
	Parallelism int
}

// PrimaryResult는 primary 단계 결과다. Failed는 context가 살아 있는 동안 실패한 target만 담는다.
// 호출자 context가 끝나 시작하지 못했거나 끝난 뒤 실패한 target은 Canceled에 따로 담는다. 호출자 취소를 실패로 세면
// 취소된 요청이 fallback·재시도·실패 metric을 일으키기 때문이다. 호출자가 직접 건 단계 timeout이 원인이라면
// Canceled를 실패로 다룰지는 호출자가 정한다.
type PrimaryResult[K any] struct {
	Attempted int
	Succeeded int
	Failed    []K
	Canceled  []K
}

func (r PrimaryResult[K]) WasCanceled() bool {
	return len(r.Canceled) > 0
}

func (r PrimaryResult[K]) HasFailures() bool {
	return len(r.Failed) > 0
}

func (r PrimaryResult[K]) AllFailed() bool {
	return r.Attempted > 0 && r.Succeeded == 0 && len(r.Failed) == r.Attempted
}

type targetOutcome uint8

const (
	targetSucceeded targetOutcome = iota
	targetFailed
	targetCanceled
)

// RunPrimary는 개별 target 실패가 나머지 target 실행을 멈추지 않게 모든 target을 시도한다.
// 호출자 context가 이미 끝났으면 남은 target은 시작하지 않고 Canceled에 담는다.
func (plan FetchPlan[K]) RunPrimary(ctx context.Context, keys []K, run func(context.Context, K) error) PrimaryResult[K] {
	outcomes := make([]targetOutcome, len(keys))

	if plan.Parallelism <= 1 {
		for i, key := range keys {
			outcomes[i] = runTarget(ctx, key, run)
		}
	} else {
		plan.runParallel(ctx, keys, run, outcomes)
	}

	return summarizeOutcomes(keys, outcomes)
}

// 각 goroutine은 자기 index 칸에만 결과를 쓰므로 outcomes에 별도 잠금이 필요 없다(wg.Wait가 쓰기를 발행한다).
// 첫 실패가 다른 target을 멈추지 않도록 공유 cancel을 만들지 않는다.
func (plan FetchPlan[K]) runParallel(ctx context.Context, keys []K, run func(context.Context, K) error, outcomes []targetOutcome) {
	limiter := make(chan struct{}, plan.Parallelism)

	var wg sync.WaitGroup

	for i, key := range keys {
		limiter <- struct{}{}

		wg.Go(func() {
			defer func() { <-limiter }()

			var outcome targetOutcome

			// panic은 target 실패로 센다. panicguard가 복구 사실을 로그로 남긴다.
			if err := panicguard.RunE(nil, panicguard.BackgroundTask, "fallback-fetch", func() error {
				outcome = runTarget(ctx, key, run)

				return nil
			}); err != nil {
				outcome = targetFailed
			}

			outcomes[i] = outcome
		})
	}

	wg.Wait()
}

func runTarget[K any](ctx context.Context, key K, run func(context.Context, K) error) targetOutcome {
	if ctx.Err() != nil {
		return targetCanceled
	}

	if err := run(ctx, key); err != nil {
		if ctx.Err() != nil {
			return targetCanceled
		}

		return targetFailed
	}

	return targetSucceeded
}

func summarizeOutcomes[K any](keys []K, outcomes []targetOutcome) PrimaryResult[K] {
	result := PrimaryResult[K]{
		Attempted: len(keys),
		Failed:    make([]K, 0, len(keys)),
	}

	for i, outcome := range outcomes {
		switch outcome {
		case targetSucceeded:
			result.Succeeded++
		case targetFailed:
			result.Failed = append(result.Failed, keys[i])
		case targetCanceled:
			result.Canceled = append(result.Canceled, keys[i])
		}
	}

	return result
}
