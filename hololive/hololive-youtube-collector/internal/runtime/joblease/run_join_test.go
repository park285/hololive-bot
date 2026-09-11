package joblease

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

func TestWaitRunResultPrefersCompletedRunnerAfterDeadline(t *testing.T) {
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()

	for _, callbackErr := range []error{nil, fmt.Errorf("callback request: %w", context.DeadlineExceeded)} {
		result := make(chan error, 1)
		result <- callbackErr

		joined, got := waitRunResult(ctx, result)
		if !joined || !errors.Is(got, callbackErr) {
			t.Fatalf("waitRunResult() = (%t, %v), want (true, %v)", joined, got, callbackErr)
		}
	}

	joined, got := waitRunResult(ctx, make(chan error))
	if joined || !errors.Is(got, context.DeadlineExceeded) {
		t.Fatalf("waitRunResult() = (%t, %v), want false and deadline", joined, got)
	}
}

type cleanupJoinCase struct {
	name    string
	outcome LeaseRunOutcome
	reason  ReleaseReason
	cause   error
}

func TestCleanupJoinDistinguishesCallbackDeadline(t *testing.T) {
	renewErr := errors.New("renew database unavailable")
	for _, testCase := range []cleanupJoinCase{
		{name: "parent cancel", outcome: LeaseRunReleasedAfterParentCancel, reason: ReleaseShutdown, cause: context.Canceled},
		{name: "renew failure", outcome: LeaseRunReleasedAfterRenewFailure, reason: ReleaseRenewFail, cause: renewErr},
		{name: "fence loss", outcome: LeaseRunFenceLost, cause: ErrFenceLost},
	} {
		for _, completed := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/completed=%t", testCase.name, completed), func(t *testing.T) {
				checkCleanupJoin(t, testCase, completed)
			})
		}
	}
}

func checkCleanupJoin(t *testing.T, testCase cleanupJoinCase, completed bool) {
	t.Helper()

	config := testConfig()

	config.CleanupTimeout = time.Millisecond

	repository := &Repository{config: config}
	lease := &fakeLease{}
	ctx, cancel := context.WithCancel(t.Context())

	defer cancel()

	result := make(chan error, 1)
	callbackErr := fmt.Errorf("callback request: %w", context.DeadlineExceeded)
	// Fence의 선행 결과 확인 이후, 취소 시점에 callback 종료를 확정합니다.
	cancelRunner := func() {
		cancel()

		if completed {
			result <- callbackErr
		}
	}

	var got LeaseRunResult

	if testCase.reason == ReleaseShutdown {
		cancel()

		got = repository.handleRunCancel(ctx, cancelRunner, lease, result)
	} else {
		got = repository.finishRenewFailure(ctx, cancelRunner, lease, result, testCase.cause)
	}

	checkCleanupJoinResult(t, testCase, completed, got, callbackErr)

	wantReleases := int32(1)

	if testCase.reason == "" {
		wantReleases = 0
	}

	if lease.releaseCalls.Load() != wantReleases || lease.lastRelease != testCase.reason {
		t.Errorf("release calls = %d reason = %q, want %d %q", lease.releaseCalls.Load(), lease.lastRelease, wantReleases, testCase.reason)
	}
}

func checkCleanupJoinResult(t *testing.T, testCase cleanupJoinCase, completed bool, got LeaseRunResult, callbackErr error) {
	t.Helper()

	wantOutcome := testCase.outcome

	if !completed {
		wantOutcome = LeaseRunCleanupTimedOut
	}

	if got.Outcome != wantOutcome {
		t.Fatalf("outcome = %s, want %s: %v", got.Outcome, wantOutcome, got.Err)
	}

	if completed && !errors.Is(got.Err, testCase.cause) {
		t.Errorf("error = %v, want cause %v", got.Err, testCase.cause)
	}

	if completed && !errors.Is(got.Err, callbackErr) {
		t.Errorf("error = %v, want callback error %v", got.Err, callbackErr)
	}

	if !completed && !errors.Is(got.Err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want join deadline", got.Err)
	}
}

func TestFenceLossPreservesCallbackAfterCancel(t *testing.T) {
	for _, panicCallback := range []bool{false, true} {
		t.Run(fmt.Sprintf("panic=%t", panicCallback), func(t *testing.T) {
			config := testConfig()

			config.RenewInterval = time.Millisecond
			config.CleanupTimeout = time.Second

			repository := &Repository{config: config}
			cause := errors.New("late callback failure")
			result := repository.Run(t.Context(), &fakeLease{}, func(ctx context.Context, _ contract.LeaseProof) error {
				<-ctx.Done()

				if panicCallback {
					panic(cause)
				}

				return errors.Join(ctx.Err(), cause)
			})

			if result.Outcome != LeaseRunFenceLost || !errors.Is(result.Err, ErrFenceLost) {
				t.Fatalf("result = %#v, want fence loss", result)
			}

			if panicCallback {
				if !strings.Contains(result.Err.Error(), "recovered panic: late callback failure") {
					t.Fatalf("panic lost: %v", result.Err)
				}
			} else if !errors.Is(result.Err, cause) {
				t.Fatalf("callback cause lost: %v", result.Err)
			}
		})
	}
}

func TestParentCancelPreservesClassifiedCallbackJoinedWithCancel(t *testing.T) {
	cause := collecterr.New(collecterr.Internal, collecterr.ClassInternal, "callback invariant")

	for _, buffered := range []bool{false, true} {
		t.Run(fmt.Sprintf("buffered=%t", buffered), func(t *testing.T) {
			repository := &Repository{config: testConfig()}
			lease := &fakeLease{}
			ctx, cancel := context.WithCancel(t.Context())

			defer cancel()

			callbackErr := errors.Join(context.Canceled, cause)

			var result LeaseRunResult

			if buffered {
				cancel()

				result = repository.finishAvailableRun(ctx, cancel, lease, callbackErr)
			} else {
				result = repository.Run(ctx, lease, func(ctx context.Context, _ contract.LeaseProof) error {
					cancel()
					<-ctx.Done()

					return callbackErr
				})
			}

			if result.Outcome != LeaseRunReleasedAfterParentCancel || !errors.Is(result.Err, context.Canceled) || !errors.Is(result.Err, cause) {
				t.Fatalf("result = %#v, want parent cancel and callback cause", result)
			}

			if lease.releaseCalls.Load() != 1 {
				t.Fatalf("release calls = %d, want 1", lease.releaseCalls.Load())
			}
		})
	}
}

func TestRenewBufferedCallbackHonorsParentCancellation(t *testing.T) {
	repository := &Repository{config: testConfig()}
	lease := &fakeLease{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	cause := collecterr.New(collecterr.Internal, collecterr.ClassInternal, "callback invariant")
	result := make(chan error, 1)

	result <- errors.Join(context.Canceled, cause)

	got, done := repository.handleRunRenew(ctx, cancel, lease, result)
	if !done || got.Outcome != LeaseRunReleasedAfterParentCancel || !errors.Is(got.Err, cause) {
		t.Fatalf("renew buffered result = %#v done=%t, want parent cancellation and callback cause", got, done)
	}

	if lease.releaseCalls.Load() != 1 || lease.lastRelease != ReleaseShutdown {
		t.Fatalf("release = %d/%s, want one shutdown release", lease.releaseCalls.Load(), lease.lastRelease)
	}
}
