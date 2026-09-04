package youtubedispatch

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress"
	ytlifecycle "github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type lifecycleTestSender struct {
	calls atomic.Int32
}

func TestLifecycleProviderFailureTreatsKaringStatusFailureAsKnown(t *testing.T) {
	t.Parallel()

	kind, reason, retryAfter := lifecycleProviderFailure(egress.ErrKaringStatusFailed, lifecycleReasonKaring)

	if kind != ytlifecycle.FailurePermanent || reason != lifecycleReasonKaring || retryAfter != 0 {
		t.Fatalf("lifecycleProviderFailure() = %v, %q, %s; want permanent, %q, 0", kind, reason, retryAfter, lifecycleReasonKaring)
	}
}

func (s *lifecycleTestSender) SendMessage(context.Context, string, string) error {
	s.calls.Add(1)

	return nil
}

type lifecycleTransitionSpy struct {
	beginCalls          atomic.Int32
	startedFailureCalls atomic.Int32
	completeCalls       atomic.Int32
	complete            store.ApplyResult
	completeErr         error
}

func (s *lifecycleTransitionSpy) PrepareClaimed(
	context.Context,
	[]domain.YouTubeNotificationDelivery,
	map[int64]domain.YouTubeNotificationOutbox,
) (store.PrepareClaimsResult, error) {
	return store.PrepareClaimsResult{}, nil
}

func (s *lifecycleTransitionSpy) BeginSending(
	context.Context,
	[]domain.YouTubeNotificationDelivery,
	map[int64]domain.YouTubeNotificationOutbox,
) (store.StartedOperation, store.ApplyResult, error) {
	s.beginCalls.Add(1)

	return store.StartedOperation{}, store.ApplyResult{Outcome: store.ApplyApplied}, nil
}

func (s *lifecycleTransitionSpy) ApplyPreparedFailure(
	context.Context,
	[]domain.YouTubeNotificationDelivery,
	map[int64]domain.YouTubeNotificationOutbox,
	ytlifecycle.FailureKind,
	ytlifecycle.Reason,
	time.Duration,
) (store.ApplyResult, error) {
	return store.ApplyResult{Outcome: store.ApplyApplied}, nil
}

func (s *lifecycleTransitionSpy) ApplyStartedFailure(
	context.Context,
	store.StartedOperation,
	ytlifecycle.FailureKind,
	ytlifecycle.Reason,
	time.Duration,
) (store.ApplyResult, error) {
	s.startedFailureCalls.Add(1)

	return store.ApplyResult{Outcome: store.ApplyApplied}, nil
}

func (s *lifecycleTransitionSpy) CompleteSent(
	context.Context,
	store.StartedOperation,
	[]dispatchstate.ClaimToken,
) (store.ApplyResult, error) {
	s.completeCalls.Add(1)

	return s.complete, s.completeErr
}

func TestDispatchClaimedDeliveryResponseLostAfterProviderSuccessDoesNotResend(t *testing.T) {
	t.Parallel()

	sender := &lifecycleTestSender{}
	engine, claims := newOutcomeUnknownTestEngine(sender, nil, time.Second)
	transition := &lifecycleTransitionSpy{
		complete:    store.ApplyResult{Outcome: store.ApplyIndeterminate},
		completeErr: errors.New("commit response unavailable"),
	}

	engine.transition = transition

	row := domain.YouTubeNotificationDelivery{
		ID: 101, OutboxID: 1, RoomID: testRoom1, Status: domain.OutboxStatusPending,
		RowVersion: 1, LockedAt: new(time.Now().UTC()),
	}
	outbox := domain.YouTubeNotificationOutbox{
		ID: 1, ChannelID: testChannelCh1, Kind: domain.OutboxKindNewVideo,
		ContentID: "video-lifecycle-response-lost", Payload: `{"video_id":"video-lifecycle-response-lost"}`,
	}
	result := dispatchstate.DispatchResult{FailureBuckets: make(map[string][]int64)}

	var mu sync.Mutex

	engine.dispatchClaimedDeliveryRow(
		t.Context(),
		&row,
		&outbox,
		map[int64]string{outbox.ID: testMessageHello},
		map[int64]bool{},
		nil,
		&result,
		&mu,
	)

	if got := sender.calls.Load(); got != 1 {
		t.Fatalf("provider calls = %d, want 1", got)
	}

	if got := transition.beginCalls.Load(); got != 1 {
		t.Fatalf("begin calls = %d, want 1", got)
	}

	if got := transition.completeCalls.Load(); got != 1 {
		t.Fatalf("complete calls = %d, want 1", got)
	}

	if len(result.SuccessDeliveryIDs) != 0 || result.FailedDeliveries != 0 {
		t.Fatalf("result = %#v, want no inferred terminal outcome", result)
	}

	if got := claims.releaseCalls.Load(); got != 0 {
		t.Fatalf("claim release calls = %d, want 0", got)
	}
}
