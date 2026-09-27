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
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func TestPolicyShouldRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		policy         Policy
		primaryResults int
		failedTargets  int
		want           bool
	}{
		{
			name:           "on failures",
			policy:         Policy{Trigger: TriggerOnFailures},
			primaryResults: 1,
			failedTargets:  1,
			want:           true,
		},
		{
			name:           "on empty primary",
			policy:         Policy{Trigger: TriggerOnEmptyPrimary},
			primaryResults: 0,
			failedTargets:  0,
			want:           true,
		},
		{
			name:           "on empty primary with error requires both",
			policy:         Policy{Trigger: TriggerOnEmptyPrimaryWithError},
			primaryResults: 0,
			failedTargets:  1,
			want:           true,
		},
		{
			name:           "on empty primary with error skips partial success",
			policy:         Policy{Trigger: TriggerOnEmptyPrimaryWithError},
			primaryResults: 1,
			failedTargets:  1,
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.policy.ShouldRun(tt.primaryResults, tt.failedTargets); got != tt.want {
				t.Fatalf("ShouldRun() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunPrimaryCountsRecoveredPanicAsFailure(t *testing.T) {
	t.Parallel()

	result := FetchPlan[int]{Parallelism: 2}.RunPrimary(t.Context(), []int{1, 2}, func(_ context.Context, target int) error {
		if target == 2 {
			panic("fetch panic")
		}

		return nil
	})

	if result.Succeeded != 1 {
		t.Fatalf("Succeeded = %d, want 1", result.Succeeded)
	}

	if !reflect.DeepEqual(result.Failed, []int{2}) {
		t.Fatalf("Failed = %#v, want [2]", result.Failed)
	}
}

func TestRunPrimaryRespectsParallelismLimit(t *testing.T) {
	t.Parallel()

	var (
		inFlight    atomic.Int32
		maxInFlight atomic.Int32
	)

	result := FetchPlan[int]{Parallelism: 2}.RunPrimary(t.Context(), []int{1, 2, 3, 4, 5, 6}, func(_ context.Context, _ int) error {
		current := inFlight.Add(1)

		for {
			previous := maxInFlight.Load()
			if current <= previous || maxInFlight.CompareAndSwap(previous, current) {
				break
			}
		}

		time.Sleep(10 * time.Millisecond)
		inFlight.Add(-1)

		return nil
	})

	if result.Succeeded != 6 {
		t.Fatalf("Succeeded = %d, want 6", result.Succeeded)
	}

	if observedMax := maxInFlight.Load(); observedMax > 2 {
		t.Fatalf("maxInFlight = %d, want <= 2", observedMax)
	}
}

func TestRunPrimaryCollectsFailuresInOriginalOrder(t *testing.T) {
	t.Parallel()

	result := FetchPlan[string]{Parallelism: 2}.RunPrimary(t.Context(), []string{"a", "b", "c"}, func(_ context.Context, key string) error {
		if key == "b" {
			return errors.New("boom")
		}

		return nil
	})

	if result.Attempted != 3 {
		t.Fatalf("Attempted = %d, want 3", result.Attempted)
	}

	if result.Succeeded != 2 {
		t.Fatalf("Succeeded = %d, want 2", result.Succeeded)
	}

	if !reflect.DeepEqual(result.Failed, []string{"b"}) {
		t.Fatalf("Failed = %#v, want [\"b\"]", result.Failed)
	}

	if !result.HasFailures() {
		t.Fatal("HasFailures() = false, want true")
	}

	if result.AllFailed() {
		t.Fatal("AllFailed() = true, want false")
	}
}

// 호출자 취소는 target 실패가 아니다. 실패로 세면 취소된 요청이 fallback·재시도·실패 metric을 일으킨다.
func TestRunPrimaryDoesNotCountCancellationAsFailure(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	for _, parallelism := range []int{1, 2} {
		result := FetchPlan[string]{Parallelism: parallelism}.RunPrimary(ctx, []string{"a", "b"}, func(runCtx context.Context, _ string) error {
			return runCtx.Err()
		})

		if len(result.Failed) != 0 {
			t.Fatalf("parallelism=%d Failed = %#v, want none for canceled context", parallelism, result.Failed)
		}

		if !reflect.DeepEqual(result.Canceled, []string{"a", "b"}) {
			t.Fatalf("parallelism=%d Canceled = %#v, want [a b]", parallelism, result.Canceled)
		}
	}
}
