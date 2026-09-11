package youtubedispatch

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type karingAdmissionTransition struct {
	lifecycleTransitionSpy

	preparedCalls  int
	preparedKind   lifecycle.FailureKind
	preparedResult store.ApplyResult
	preparedErr    error
	begin          func()
	beginErr       error
	startedKind    lifecycle.FailureKind
}

func (s *karingAdmissionTransition) BeginSending(context.Context, []domain.YouTubeNotificationDelivery, map[int64]domain.YouTubeNotificationOutbox) (store.StartedOperation, store.ApplyResult, error) {
	s.beginCalls.Add(1)

	if s.begin != nil {
		s.begin()
	}

	return store.StartedOperation{}, store.ApplyResult{Outcome: store.ApplyApplied}, s.beginErr
}

func (s *karingAdmissionTransition) ApplyStartedFailure(_ context.Context, _ store.StartedOperation, kind lifecycle.FailureKind, _ lifecycle.Reason, _ time.Duration) (store.ApplyResult, error) {
	s.startedFailureCalls.Add(1)

	s.startedKind = kind

	return store.ApplyResult{Outcome: store.ApplyApplied}, nil
}

func (s *karingAdmissionTransition) ApplyPreparedFailure(_ context.Context, _ []domain.YouTubeNotificationDelivery, _ map[int64]domain.YouTubeNotificationOutbox, kind lifecycle.FailureKind, _ lifecycle.Reason, _ time.Duration) (store.ApplyResult, error) {
	s.preparedCalls++

	s.preparedKind = kind

	return s.preparedResult, s.preparedErr
}

type karingCallbackSender struct {
	youtubeOutboxKaringTestSender

	send func(context.Context) error
}

func (s *karingCallbackSender) SendYouTubeOutboxKaring(ctx context.Context, _ string, _ *domain.YouTubeOutboxDispatchPayload) error {
	s.calls++
	return s.send(ctx)
}

func TestKaringAdmissionFailureRequiresConfirmedTransitionBeforeRelease(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sender := &youtubeOutboxKaringTestSender{}
		engine, claims := newOutcomeUnknownTestEngine(sender, nil, time.Second)
		transition := &karingAdmissionTransition{preparedResult: store.ApplyResult{Outcome: store.ApplyIndeterminate}, preparedErr: errors.New("commit response unavailable")}

		engine.transition = transition
		engine.karingMu.Lock()
		defer engine.karingMu.Unlock()

		result := dispatchKaringAdmissionTest(t.Context(), engine, sender)
		assertOutcomeUnknownHold(t, &result, claims)

		if sender.calls != 0 || transition.beginCalls.Load() != 0 || transition.preparedCalls != 1 {
			t.Fatalf("calls: sender=%d begin=%d prepared=%d", sender.calls, transition.beginCalls.Load(), transition.preparedCalls)
		}
	})
}

func TestKaringAdmissionWaitDoesNotConsumeProviderBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const budget = 100 * time.Millisecond

		sender := &karingCallbackSender{send: func(ctx context.Context) error {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) != budget {
				t.Errorf("remaining provider budget = %s, has deadline = %v; want %s", time.Until(deadline), ok, budget)
			}

			time.Sleep(80 * time.Millisecond)

			return ctx.Err()
		}}
		engine, _ := newOutcomeUnknownTestEngine(sender, nil, budget)
		transition := &karingAdmissionTransition{}

		transition.complete = store.ApplyResult{Outcome: store.ApplyApplied}
		engine.transition = transition
		engine.karingMu.Lock()

		done := make(chan dispatchstate.DispatchResult, 1)

		go func() { done <- dispatchKaringAdmissionTest(t.Context(), engine, sender) }()

		synctest.Wait()

		if transition.beginCalls.Load() != 0 {
			t.Fatal("BeginSending ran while admission was waiting")
		}

		time.Sleep(80 * time.Millisecond)
		engine.karingMu.Unlock()

		result := <-done
		if sender.calls != 1 || len(result.SuccessDeliveryIDs) != 1 {
			t.Fatalf("sender calls = %d, result = %+v; want one success", sender.calls, result)
		}
	})
}

func TestKaringCanceledBeforeAdmissionNeverStartsSender(t *testing.T) {
	for range 50 {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		sender := &youtubeOutboxKaringTestSender{}
		engine, _ := newOutcomeUnknownTestEngine(sender, nil, time.Second)
		transition := &karingAdmissionTransition{preparedResult: store.ApplyResult{Outcome: store.ApplyApplied}}

		engine.transition = transition
		dispatchKaringAdmissionTest(ctx, engine, sender)

		if sender.calls != 0 || transition.beginCalls.Load() != 0 || transition.preparedKind != lifecycle.FailureRetryable {
			t.Fatalf("calls: sender=%d begin=%d; prepared kind=%v", sender.calls, transition.beginCalls.Load(), transition.preparedKind)
		}

		assertKaringSlotReleased(t, engine)
	}
}

func TestKaringCanceledAfterAdmissionBeforeSenderIsRetryable(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	sender := &youtubeOutboxKaringTestSender{}
	engine, claims := newOutcomeUnknownTestEngine(sender, nil, time.Second)
	transition := &karingAdmissionTransition{begin: cancel}

	engine.transition = transition

	result := dispatchKaringAdmissionTest(ctx, engine, sender)

	if sender.calls != 0 || transition.beginCalls.Load() != 1 || transition.startedFailureCalls.Load() != 1 || transition.startedKind != lifecycle.FailureRetryable {
		t.Fatalf("calls: sender=%d begin=%d started failure=%d; failure kind=%v", sender.calls, transition.beginCalls.Load(), transition.startedFailureCalls.Load(), transition.startedKind)
	}

	if result.FailedDeliveries != 1 || claims.releaseCalls.Load() != 1 {
		t.Fatalf("result = %+v, claim releases = %d", result, claims.releaseCalls.Load())
	}

	assertKaringSlotReleased(t, engine)
}

func TestKaringProviderTimeoutPreservesUnknown(t *testing.T) {
	for _, parentDeadline := range []bool{false, true} {
		t.Run(map[bool]string{false: "send_budget", true: "parent_deadline"}[parentDeadline], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx := t.Context()

				if parentDeadline {
					var cancel context.CancelFunc

					ctx, cancel = context.WithTimeout(ctx, 20*time.Millisecond)

					defer cancel()
				}

				sender := &karingCallbackSender{send: func(ctx context.Context) error {
					<-ctx.Done()

					return ctx.Err()
				}}
				engine, claims := newOutcomeUnknownTestEngine(sender, nil, 100*time.Millisecond)
				transition := &karingAdmissionTransition{}

				engine.transition = transition

				result := dispatchKaringAdmissionTest(ctx, engine, sender)
				assertOutcomeUnknownHold(t, &result, claims)

				if sender.calls != 1 || transition.startedFailureCalls.Load() != 0 || transition.completeCalls.Load() != 0 {
					t.Fatalf("calls: sender=%d failure=%d complete=%d", sender.calls, transition.startedFailureCalls.Load(), transition.completeCalls.Load())
				}

				assertKaringSlotReleased(t, engine)
			})
		})
	}
}

func TestKaringSlotReleasedOnLifecycleFailureAndPanic(t *testing.T) {
	for _, phase := range []string{"begin", "complete", "sender", "panic"} {
		t.Run(phase, func(t *testing.T) {
			sender := &karingCallbackSender{send: func(context.Context) error { return nil }}
			engine, claims := newOutcomeUnknownTestEngine(sender, nil, time.Second)
			transition := &karingAdmissionTransition{}

			engine.transition = transition

			switch phase {
			case "begin":
				transition.beginErr = errors.New("begin failed")
			case "complete":
				transition.completeErr = errors.New("commit response unavailable")
				transition.complete = store.ApplyResult{Outcome: store.ApplyIndeterminate}
			case "sender":
				sender.send = func(context.Context) error { return egress.ErrKaringStatusFailed }
			case "panic":
				sender.send = func(context.Context) error { panic("sender panic") }
			}

			panicked := false

			func() {
				defer func() { panicked = recover() != nil }()

				dispatchKaringAdmissionTest(t.Context(), engine, sender)
			}()

			if panicked != (phase == "panic") {
				t.Fatalf("panicked = %v in phase %s", panicked, phase)
			}

			if phase == "sender" {
				if transition.startedKind != lifecycle.FailurePermanent || claims.releaseCalls.Load() != 1 {
					t.Fatal("explicit provider failure must retain permanent failure and claim release")
				}
			} else if claims.releaseCalls.Load() != 0 {
				t.Fatal("unconfirmed lifecycle outcome or panic must preserve claims")
			}

			assertKaringSlotReleased(t, engine)
		})
	}
}

func assertKaringSlotReleased(t *testing.T, engine *SendEngine) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)

	defer cancel()

	if err := engine.acquireKaringSendSlot(ctx); err != nil {
		t.Fatalf("Karing slot was not released: %v", err)
	}

	engine.karingMu.Unlock()
}

func dispatchKaringAdmissionTest(ctx context.Context, engine *SendEngine, sender YouTubeOutboxKaringSender) dispatchstate.DispatchResult {
	return dispatchKaringRoomTest(ctx, engine, sender, testRoomOne)
}

func dispatchKaringRoomTest(ctx context.Context, engine *SendEngine, sender YouTubeOutboxKaringSender, roomID string) dispatchstate.DispatchResult {
	rows := []domain.YouTubeNotificationDelivery{{ID: 101, OutboxID: 1, RoomID: roomID}}
	outboxes := []domain.YouTubeNotificationOutbox{{
		ID: 1, ChannelID: "UCvideo", Kind: domain.OutboxKindNewVideo,
		ContentID: "video-1", Payload: `{"video_id":"video-1","title":"video 1"}`,
	}}
	claims := []dispatchstate.ClaimToken{outcomeUnknownClaimToken(&outboxes[0])}
	result := dispatchstate.DispatchResult{FailureBuckets: make(map[string][]int64)}

	var mu sync.Mutex

	engine.dispatchClaimedKaring(ctx, sender, roomID, "UCvideo", domain.OutboxKindNewVideo, rows, outboxes, claims, "", &result, &mu)

	return result
}

type blockingKaringCompletionTransition struct {
	lifecycleTransitionSpy

	entered chan struct{}
	release chan struct{}
}

func (s *blockingKaringCompletionTransition) CompleteSent(context.Context, store.StartedOperation, []dispatchstate.ClaimToken) (store.ApplyResult, error) {
	if s.completeCalls.Add(1) == 1 {
		close(s.entered)
		<-s.release
	}

	return store.ApplyResult{Outcome: store.ApplyApplied}, nil
}

func TestKaringCompletionDoesNotBlockAnotherRoomSender(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sender := &youtubeOutboxKaringTestSender{}
		engine, _ := newOutcomeUnknownTestEngine(sender, nil, time.Second)
		transition := &blockingKaringCompletionTransition{entered: make(chan struct{}), release: make(chan struct{})}

		engine.transition = transition

		firstDone := make(chan dispatchstate.DispatchResult, 1)
		secondDone := make(chan dispatchstate.DispatchResult, 1)

		go func() { firstDone <- dispatchKaringRoomTest(t.Context(), engine, sender, testRoomOne) }()

		<-transition.entered

		go func() { secondDone <- dispatchKaringRoomTest(t.Context(), engine, sender, "room-2") }()

		synctest.Wait()

		sender.mu.Lock()

		callsWhileCompleting := sender.calls

		sender.mu.Unlock()
		close(transition.release)

		first := <-firstDone
		second := <-secondDone

		if callsWhileCompleting != 2 {
			t.Fatalf("sender calls while first room completion was blocked = %d, want 2", callsWhileCompleting)
		}

		if len(first.SuccessDeliveryIDs) != 1 || len(second.SuccessDeliveryIDs) != 1 {
			t.Fatalf("room results = %+v, %+v; want both sent", first, second)
		}
	})
}

func TestKaringAdmissionTimeoutRetriesWithoutBeginningSend(t *testing.T) {
	sender := &youtubeOutboxKaringTestSender{}
	engine, claims := newOutcomeUnknownTestEngine(sender, nil, 20*time.Millisecond)
	transition := &karingAdmissionTransition{preparedResult: store.ApplyResult{Outcome: store.ApplyApplied}}

	engine.transition = transition
	engine.karingMu.Lock()
	defer engine.karingMu.Unlock()

	result := dispatchKaringAdmissionTest(t.Context(), engine, sender)
	if sender.calls != 0 || transition.beginCalls.Load() != 0 {
		t.Fatalf("sender calls = %d, begin calls = %d; want 0, 0", sender.calls, transition.beginCalls.Load())
	}

	if transition.preparedCalls != 1 || transition.preparedKind != lifecycle.FailureRetryable {
		t.Fatalf("prepared failure = %d, %v; want 1 retryable", transition.preparedCalls, transition.preparedKind)
	}

	if result.FailedDeliveries != 1 || claims.releaseCalls.Load() != 1 {
		t.Fatalf("failed = %d, claim releases = %d; want 1, 1", result.FailedDeliveries, claims.releaseCalls.Load())
	}

	if transition.startedFailureCalls.Load() != 0 || transition.completeCalls.Load() != 0 {
		t.Fatal("admission failure must not finalize a started operation")
	}
}
