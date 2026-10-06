package youtubedispatch

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/sendoutcome"
)

func TestProviderFailurePreservesUnknownBeforeKnownFailure(t *testing.T) {
	engine := &SendEngine{}
	source := errors.Join(sendoutcome.ErrHandoffFailed, sendoutcome.ErrHandoffOutcomeUnknown)

	for _, err := range []error{source, engine.wrapDeliverySendError(t.Context(), source)} {
		kind, _, _ := lifecycleProviderFailure(err)
		if kind != lifecycle.FailureOutcomeUnknown {
			t.Fatalf("unknown evidence downgraded: %v", kind)
		}
	}
}

func TestTimedOutHandoffFailureDoesNotPersistKnownFailure(t *testing.T) {
	transition := &lifecycleTransitionSpy{}
	engine := &SendEngine{transition: transition, logger: slog.New(slog.DiscardHandler)}
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(errDeliverySendTimeout)

	err := engine.wrapDeliverySendError(ctx, sendoutcome.ErrHandoffFailed)
	result := &dispatchstate.DispatchResult{}

	var mu sync.Mutex

	engine.applyPerRoomLifecycleFailure(t.Context(), store.StartedOperation{}, &domain.YouTubeNotificationDelivery{}, deliverySendRequest{}, err, result, &mu)

	if transition.startedFailureCalls.Load() != 0 {
		t.Fatal("outcome unknown reached known-failure persistence")
	}
}
