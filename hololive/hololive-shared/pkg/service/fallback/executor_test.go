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
	"slices"
	"sync"
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

func TestExecuteCollectsSuccessesAndFailures(t *testing.T) {
	t.Parallel()

	var (
		successes []int
		mu        sync.Mutex
	)
	summary := Execute(context.Background(), FetchPlan[int, string]{
		Targets:     []int{1, 2, 3},
		Parallelism: 2,
		Fetch: func(_ context.Context, target int) (string, error) {
			if target == 2 {
				return "", errors.New("fail")
			}
			return "ok", nil
		},
		OnSuccess: func(target int, _ string) {
			mu.Lock()
			successes = append(successes, target)
			mu.Unlock()
		},
	})

	slices.Sort(successes)
	slices.Sort(summary.FailedTargets)

	if summary.SuccessCount != 2 {
		t.Fatalf("SuccessCount = %d, want 2", summary.SuccessCount)
	}
	if summary.FailedCount != 1 {
		t.Fatalf("FailedCount = %d, want 1", summary.FailedCount)
	}
	if !reflect.DeepEqual(successes, []int{1, 3}) {
		t.Fatalf("successes = %#v, want [1 3]", successes)
	}
	if !reflect.DeepEqual(summary.FailedTargets, []int{2}) {
		t.Fatalf("FailedTargets = %#v, want [2]", summary.FailedTargets)
	}
}

func TestExecuteRespectsParallelismLimit(t *testing.T) {
	t.Parallel()

	var inFlight int32
	var maxInFlight int32

	summary := Execute(context.Background(), FetchPlan[int, string]{
		Targets:     []int{1, 2, 3, 4, 5, 6},
		Parallelism: 2,
		Fetch: func(_ context.Context, target int) (string, error) {
			current := atomic.AddInt32(&inFlight, 1)
			for {
				previous := atomic.LoadInt32(&maxInFlight)
				if current <= previous || atomic.CompareAndSwapInt32(&maxInFlight, previous, current) {
					break
				}
			}

			time.Sleep(10 * time.Millisecond)
			atomic.AddInt32(&inFlight, -1)
			return "ok", nil
		},
	})

	if summary.SuccessCount != 6 {
		t.Fatalf("SuccessCount = %d, want 6", summary.SuccessCount)
	}
	if max := atomic.LoadInt32(&maxInFlight); max > 2 {
		t.Fatalf("maxInFlight = %d, want <= 2", max)
	}
}

func TestRunPrimaryCollectsFailuresInOriginalOrder(t *testing.T) {
	t.Parallel()

	result := RunPrimary(context.Background(), []string{"a", "b", "c"}, FetchPlan[string, struct{}]{Parallelism: 2}, func(_ context.Context, key string) error {
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
