package holodexprovider

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunOrgFetchesCountsRecoveredPanicAsFailure(t *testing.T) {
	t.Parallel()

	result := runOrgFetches(t.Context(), 2, []string{"a", "b"}, func(_ context.Context, org string) error {
		if org == "b" {
			panic("fetch panic")
		}

		return nil
	})

	if result.Succeeded != 1 || !slices.Equal(result.Failed, []string{"b"}) {
		t.Fatalf("result = %+v, want 1 success and failed [b]", result)
	}
}

func TestRunOrgFetchesRespectsParallelismLimit(t *testing.T) {
	t.Parallel()

	var inFlight, maxInFlight atomic.Int32

	result := runOrgFetches(t.Context(), 2, []string{"a", "b", "c", "d", "e", "f"}, func(context.Context, string) error {
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

func TestRunOrgFetchesCollectsFailuresInOriginalOrder(t *testing.T) {
	t.Parallel()

	result := runOrgFetches(t.Context(), 2, []string{"a", "b", "c", "d"}, func(_ context.Context, org string) error {
		if org == "b" || org == "d" {
			return errors.New("boom")
		}

		return nil
	})

	if result.Attempted != 4 || result.Succeeded != 2 || !slices.Equal(result.Failed, []string{"b", "d"}) {
		t.Fatalf("result = %+v, want 4 attempted, 2 succeeded, failed [b d]", result)
	}
}

// 호출자 취소는 org 실패가 아니다. 실패로 세면 취소된 요청이 공식 일정 fallback·재시도·실패 metric을 일으킨다.
func TestRunOrgFetchesDoesNotCountCancellationAsFailure(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	for _, parallelism := range []int{1, 2} {
		result := runOrgFetches(ctx, parallelism, []string{"a", "b"}, func(runCtx context.Context, _ string) error {
			return runCtx.Err()
		})

		if len(result.Failed) != 0 || !slices.Equal(result.Canceled, []string{"a", "b"}) {
			t.Fatalf("parallelism=%d result = %+v, want no failures and canceled [a b]", parallelism, result)
		}
	}
}

func TestStreamPrimaryOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result orgFetchResult
		want   string
	}{
		{name: "skipped", result: orgFetchResult{}, want: "skipped"},
		{name: "canceled", result: orgFetchResult{Attempted: 3, Succeeded: 1, Canceled: []string{"b", "c"}}, want: "canceled"},
		{name: "success", result: orgFetchResult{Attempted: 3, Succeeded: 3}, want: "success"},
		{name: "partial", result: orgFetchResult{Attempted: 3, Succeeded: 2, Failed: []string{"c"}}, want: "partial"},
		{name: "empty", result: orgFetchResult{Attempted: 3}, want: "empty"},
		{name: "failed", result: orgFetchResult{Attempted: 3, Failed: []string{"a", "b", "c"}}, want: "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := streamPrimaryOutcome(tt.result); got != tt.want {
				t.Fatalf("streamPrimaryOutcome() = %q, want %q", got, tt.want)
			}
		})
	}
}
