package youtubedispatch

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ytlifecycle "github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/sendoutcome"
)

type lifecycleTestSender struct {
	calls atomic.Int32
}

// Markdown handoff 실패 확정은 전송 불명이 아니라 알려진 영구 실패다.
func TestLifecycleProviderFailureTreatsReplyHandoffFailureAsKnown(t *testing.T) {
	t.Parallel()

	kind, reason, retryAfter := lifecycleProviderFailure(sendoutcome.ErrHandoffFailed)

	if kind != ytlifecycle.FailurePermanent || reason != lifecycleReasonUnknownError || retryAfter != 0 {
		t.Fatalf("lifecycleProviderFailure() = %v, %q, %s; want permanent, %q, 0", kind, reason, retryAfter, lifecycleReasonUnknownError)
	}
}

func (s *lifecycleTestSender) SendMessage(context.Context, string, string) error {
	s.calls.Add(1)

	return nil
}

type lifecycleTransitionSpy struct {
	beginCalls          atomic.Int32
	beginErr            error
	startedFailureCalls atomic.Int32
	completeCalls       atomic.Int32
	complete            store.ApplyResult
	completeErr         error

	modesMu sync.Mutex
	modes   []string

	preparedFailuresMu sync.Mutex
	preparedFailures   []string
}

// recordMode는 전이 호출이 TransitionStore에 넘긴 발송 방식을 "operation:mode" 형태로 모은다.
func (s *lifecycleTransitionSpy) recordMode(operation string, mode store.DeliveryMode) {
	s.modesMu.Lock()
	defer s.modesMu.Unlock()

	s.modes = append(s.modes, operation+":"+string(mode))
}

func (s *lifecycleTransitionSpy) recordedModes() []string {
	s.modesMu.Lock()
	defer s.modesMu.Unlock()

	return append([]string(nil), s.modes...)
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

	if s.beginErr != nil {
		return store.StartedOperation{}, store.ApplyResult{Outcome: store.ApplyIndeterminate}, s.beginErr
	}

	return store.StartedOperation{}, store.ApplyResult{Outcome: store.ApplyApplied}, nil
}

func (s *lifecycleTransitionSpy) ApplyPreparedFailure(
	_ context.Context,
	_ []domain.YouTubeNotificationDelivery,
	_ map[int64]domain.YouTubeNotificationOutbox,
	kind ytlifecycle.FailureKind,
	reason ytlifecycle.Reason,
	_ time.Duration,
	mode store.DeliveryMode,
) (store.ApplyResult, error) {
	s.recordMode("prepared_failure", mode)
	s.recordPreparedFailure(kind, reason, mode)

	return store.ApplyResult{Outcome: store.ApplyApplied}, nil
}

func (s *lifecycleTransitionSpy) ApplyStartedFailure(
	_ context.Context,
	_ store.StartedOperation,
	_ ytlifecycle.FailureKind,
	_ ytlifecycle.Reason,
	_ time.Duration,
	mode store.DeliveryMode,
) (store.ApplyResult, error) {
	s.startedFailureCalls.Add(1)
	s.recordMode("started_failure", mode)

	return store.ApplyResult{Outcome: store.ApplyApplied}, nil
}

func (s *lifecycleTransitionSpy) CompleteSent(
	_ context.Context,
	_ store.StartedOperation,
	_ []dispatchstate.ClaimToken,
	mode store.DeliveryMode,
) (store.ApplyResult, error) {
	s.completeCalls.Add(1)
	s.recordMode("complete_sent", mode)

	return s.complete, s.completeErr
}

func TestDispatchClaimedDeliveryRequiresConfirmedBeginAndCompletion(t *testing.T) {
	t.Parallel()

	row := domain.YouTubeNotificationDelivery{ID: 101, OutboxID: 1, RoomID: testRoom1}
	outbox := domain.YouTubeNotificationOutbox{
		ID: 1, ChannelID: testChannelCh1, Kind: domain.OutboxKindNewVideo,
		ContentID: "video-lifecycle-confirmed", Payload: `{"video_id":"video-lifecycle-confirmed"}`,
	}

	for _, tc := range []struct {
		name          string
		beginErr      error
		wantSend      int32
		wantComplete  int32
		wantSuccessID bool
	}{
		{name: "begin unconfirmed", beginErr: errors.New("begin commit result unavailable")},
		{name: "completed", wantSend: 1, wantComplete: 1, wantSuccessID: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sender := &lifecycleTestSender{}
			engine, _ := newOutcomeUnknownTestEngine(sender, nil, time.Second)

			transition := &lifecycleTransitionSpy{
				beginErr: tc.beginErr,
				complete: store.ApplyResult{Outcome: store.ApplyApplied},
			}

			engine.transition = transition

			result := dispatchstate.DispatchResult{FailureBuckets: make(map[string][]int64)}

			var mu sync.Mutex

			engine.dispatchClaimedDeliveryRow(t.Context(), &row, &outbox,
				map[int64]string{outbox.ID: testMessageHello}, nil, nil, &result, &mu)

			if got := sender.calls.Load(); got != tc.wantSend {
				t.Fatalf("sender calls = %d, want %d", got, tc.wantSend)
			}

			if got := transition.beginCalls.Load(); got != 1 {
				t.Fatalf("begin calls = %d, want 1", got)
			}

			if got := transition.completeCalls.Load(); got != tc.wantComplete {
				t.Fatalf("complete calls = %d, want %d", got, tc.wantComplete)
			}

			if got := len(result.SuccessDeliveryIDs); (got == 1) != tc.wantSuccessID {
				t.Fatalf("success delivery IDs = %v, want success=%v", result.SuccessDeliveryIDs, tc.wantSuccessID)
			}
		})
	}
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

func (s *lifecycleTransitionSpy) LoadFrozenRequests(context.Context, []int64) ([]store.FrozenRequest, error) {
	return nil, nil
}

func (s *lifecycleTransitionSpy) FreezeRequest(_ context.Context, rows []domain.YouTubeNotificationDelivery, request store.FrozenRequest) (store.FrozenRequest, error) {
	request.MemberIDs = collectDeliveryIDs(rows)
	return request, nil
}

func (s *lifecycleTransitionSpy) AdvanceRequestGeneration(_ context.Context, _ store.StartedOperation, request store.FrozenRequest) (store.FrozenRequest, error) {
	request.Generation++
	return request, nil
}

func (s *lifecycleTransitionSpy) DeferFollower(context.Context, store.DeferCommand) (store.ApplyResult, error) {
	return store.ApplyResult{Outcome: store.ApplyApplied}, nil
}

func (s *lifecycleTransitionSpy) FreezeFallbackRequests(_ context.Context, _ store.StartedOperation, requests []store.FrozenRequest) ([]store.FrozenRequest, error) {
	return requests, nil
}

// recordPreparedFailure는 준비 단계 실패 전이를 "kind/reason/mode" 형태로 모은다.
func (s *lifecycleTransitionSpy) recordPreparedFailure(kind ytlifecycle.FailureKind, reason ytlifecycle.Reason, mode store.DeliveryMode) {
	s.preparedFailuresMu.Lock()
	defer s.preparedFailuresMu.Unlock()

	s.preparedFailures = append(s.preparedFailures, fmt.Sprintf("%v/%v/%v", kind, reason, mode))
}

func (s *lifecycleTransitionSpy) recordedPreparedFailures() []string {
	s.preparedFailuresMu.Lock()
	defer s.preparedFailuresMu.Unlock()

	return slices.Clone(s.preparedFailures)
}
