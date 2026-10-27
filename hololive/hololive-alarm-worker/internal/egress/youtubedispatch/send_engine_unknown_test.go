package youtubedispatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/park285/iris-client-go/v4/iris"
	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendoutcome"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestProviderFailurePreservesUnknownBeforeKnownFailure(t *testing.T) {
	engine := &SendEngine{}
	source := errors.Join(iris.ErrPermanent, sendoutcome.ErrAdmissionOutcomeUnknown)

	for _, err := range []error{source, engine.wrapDeliverySendError(t.Context(), source)} {
		kind, _, _ := lifecycleProviderFailure(err)
		if kind != lifecycle.FailureOutcomeUnknown {
			t.Fatalf("unknown evidence downgraded: %v", kind)
		}
	}
}

// ErrNotSent는 iris-client-go v4에서 ErrTransport가 아니지만 전송 전 확정 실패이므로 결과 불명이 아니라 재시도 대상이다.
func TestProviderFailureTreatsNotSentAsRetryableTransport(t *testing.T) {
	err := fmt.Errorf("%w: h3 egress denied", iris.ErrNotSent)

	kind, reason, _ := lifecycleProviderFailure(err)
	if kind != lifecycle.FailureRetryable || reason != lifecycleReasonTransport {
		t.Fatalf("lifecycleProviderFailure(ErrNotSent) = (%v, %v), want retryable transport", kind, reason)
	}

	if got := providerAttemptOutcome(err, true); got != workercontract.AttemptFailed {
		t.Fatalf("providerAttemptOutcome(ErrNotSent) = %v, want failed", got)
	}
}

func TestTimedOutKnownFailureDoesNotPersistKnownFailure(t *testing.T) {
	transition := &lifecycleTransitionSpy{}
	engine := &SendEngine{transition: transition, logger: slog.New(slog.DiscardHandler)}
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(errDeliverySendTimeout)

	err := engine.wrapDeliverySendError(ctx, iris.ErrPermanent)
	result := &deliveryDispatchResult{}

	engine.applyPerRoomLifecycleFailure(t.Context(), store.StartedOperation{}, &domain.YouTubeNotificationDelivery{}, deliverySendRequest{}, err, result)

	if transition.startedFailureCalls.Load() != 0 {
		t.Fatal("outcome unknown reached known-failure persistence")
	}
}
